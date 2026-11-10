#!/usr/bin/env python3
"""修补基础镜像里打包的 Selkies 前端。

改的必须是 selkies-dashboard：nginx 的 root 是 /usr/share/selkies/web，但该目录在
容器启动时由 init-nginx 用 `rm -Rf web && cp -a $DASHBOARD web` 重建，只有改源目录
才能带进最终产物；改 web 会在启动时被覆盖掉。

bundle 文件名带内容哈希、随上游版本变化，所以按内容锚点定位而不是文件名。每处都先
断言锚点出现次数，上游改写法时直接失败，而不是悄悄产出一个坏镜像。

一、解码器提示
  上游把解码器配置写死成 hardwareAcceleration:"prefer-software"。真机 WebCodecs 实测：
  带这个提示时所有配置（含 1280x720）一律 supported:false，不传该字段则全部 true。
  默认 encoder 是 x264enc，走分片（stripe）解码器：isConfigSupported 失败时只打一行
  "VNC stripe decoder config not supported" 并丢掉解码器，下一帧重建再失败，屏幕始终
  没有画面，遮罩停在 "Waiting for stream..."。因此把该字段整条删掉。

二、viewport 禁止缩放
  index.html 的 viewport 写死 maximum-scale=1,user-scalable=no，浏览器直接禁止捏合缩放。

三、两指手势被转成滚轮
  _handleTouchEvent 的两指分支把 h 置 true，函数结尾
  `h&&this.element.contains(s.target)&&s.preventDefault()` 因此取消浏览器默认手势。
  给结尾加 `!this._isTwoFingerGesture`，两指手势不再被拦。

四、touchstart 也不能拦
  视频画面被 input#overlayInput 整块盖住（opacity:0、pointer-events:auto），单指的
  touchstart 落在这里同样被 preventDefault。Chromium 一旦看到首指的 touchstart 被取消，
  就认定整条手势归页面所有，第二根手指落下时不再启动 pinch-zoom —— 这才是「只有画面
  上下黑边能缩放、画面里不能」的真正原因：黑边落在 div.video-container 上，没有监听。
  所以 touchstart 一律放行；单指点击/拖动/长按仍由 touchend/touchmove 分支处理，实测
  远程左键、拖动、右键（长按）都不受影响。

五、捏合期间不要发滚轮
  两指分支原来按质心位移发 _triggerMouseWheel。pinch-zoom 生效后 visualViewport 跟着
  移动，质心位移会被误判成滚动，缩放同时还在滚远端画面。改为：两指间距变化超过 8px，
  或页面已处于缩放态（visualViewport.scale 与手势起始值不同），都不发滚轮消息。这样
  未缩放时两指上下滑仍是滚动，缩放后两指平移交给浏览器自己平移 viewport。
"""

import glob
import os
import sys

ROOT = os.environ.get('SELKIES_DIR', '/usr/share/selkies/selkies-dashboard')

bundles = glob.glob(os.path.join(ROOT, 'assets', 'index-*.js'))
if len(bundles) != 1:
    sys.exit(f'期望唯一 bundle，实际 {len(bundles)} 个：{bundles}')
JS = bundles[0]
HTML = os.path.join(ROOT, 'index.html')

src = open(JS, encoding='utf-8').read()


def sub(old, new, label, count=1):
    """把 old 替换成 new，并断言 old 恰好出现 count 次。"""
    global src
    found = src.count(old)
    if found != count:
        sys.exit(f'[{label}] 锚点期望 {count} 处，实际 {found} 处，上游写法已变化')
    src = src.replace(old, new)


# 一、解码器提示
sub(',hardwareAcceleration:"prefer-software"', '', '解码器提示', count=2)
if 'prefer-software' in src:
    sys.exit('[解码器提示] 删除后仍有残留')

# 三、两指手势在函数结尾被 preventDefault
STOCK_END = 'h&&this.element.contains(s.target)&&s.preventDefault()'
TWO_FINGER_END = 'h&&!this._isTwoFingerGesture&&this.element.contains(s.target)&&s.preventDefault()'
sub(STOCK_END, TWO_FINGER_END, '两指手势')

# 四、touchstart 放行
sub(
    TWO_FINGER_END,
    'h&&u!=="touchstart"&&!this._isTwoFingerGesture&&this.element.contains(s.target)&&s.preventDefault()',
    'touchstart 放行',
)

# 五、捏合期间不发滚轮
sub(
    'if(this._touchScrollLastCentroid){const E=D-this._touchScrollLastCentroid.x,H=y-this._touchScrollLastCentroid.y,W=2;',
    'const sp=Math.hypot(v[0].currentX-v[1].currentX,v[0].currentY-v[1].currentY),'
    'ps=window.visualViewport?window.visualViewport.scale:1,'
    'zo=this._touchStartScale!==void 0&&Math.abs(ps-this._touchStartScale)>0.01,'
    'st=this._touchScrollLastSpan!==void 0&&Math.abs(sp-this._touchScrollLastSpan)<8&&!zo;'
    'if(this._touchStartScale===void 0)this._touchStartScale=ps;'
    'if(this._touchScrollLastCentroid&&st){const E=D-this._touchScrollLastCentroid.x,'
    'H=y-this._touchScrollLastCentroid.y,W=2;',
    '捏合抑制滚轮',
)
sub(
    '}this._touchScrollLastCentroid={x:D,y}',
    '}this._touchScrollLastCentroid={x:D,y},this._touchScrollLastSpan=sp',
    '捏合记录间距',
)
sub(
    'D===0&&(this._activeTouchIdentifier=null',
    'D===0&&(this._touchStartScale=void 0,this._activeTouchIdentifier=null',
    '捏合复位',
)

open(JS, 'w', encoding='utf-8').write(src)

# 二、viewport
html = open(HTML, encoding='utf-8').read()
if html.count('maximum-scale=1,user-scalable=no') != 1:
    sys.exit('[viewport] 锚点期望 1 处，上游写法已变化')
html = html.replace('maximum-scale=1,user-scalable=no', 'maximum-scale=10,user-scalable=yes')
open(HTML, 'w', encoding='utf-8').write(html)

print('Selkies 前端补丁完成')
