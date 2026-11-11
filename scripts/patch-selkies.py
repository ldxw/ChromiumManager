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

六、虚拟键盘按钮要跟随可视区
  右下角的浮动键盘按钮（class .virtual-keyboard-button，50x50，默认 right/bottom 20px）
  用的是 position:fixed。fixed 相对 layout viewport 定位，而捏合缩放改变的是
  visualViewport —— 放大并平移后按钮仍钉在 layout viewport 的右下角，实际已经滑出屏幕，
  人看不到也点不到，就再也呼不出键盘。上游没有任何 visualViewport 处理。
  按钮本身可拖拽（React 内联 right/bottom），所以不改 right/bottom，只叠加 transform：
  未缩放时位移为 0，行为与上游完全一致。样式与脚本注入 index.html 末尾，不改 bundle。
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

# 六、虚拟键盘按钮跟随可视区
#
# 按钮由 bundle 里的 React 组件渲染，选择器只用它自带的 data 属性与类名，
# 不依赖带哈希的类名或 bundle 文本，所以不必改 JS bundle。
#
# 按钮本身是可拖拽的：React 内联 style 写着 position:fixed 与 right/bottom，
# 拖动后更新这两个值。所以补丁**不碰 right/bottom**，只叠加一个 transform，
# 且取「边距 = 当前的 right / bottom」，未缩放时位移正好为 0，与上游行为完全一致。
#
# 定位算法：
#   position:fixed 钉在 layout viewport 上，捏合缩放不改变它的 layout 位置。
#   visualViewport.width/height 是可视区在 layout 坐标下的尺寸（随 scale 变小），
#   offsetLeft/offsetTop 是可视区左上角在 layout 坐标里的位置，scale 是缩放倍数。
#   设 layout viewport 为 Lw×Lh、按钮布局尺寸 w×h、当前 right/bottom 为 R/B。
#   按钮自然左上角 x0 = Lw - R - w，屏幕上出现在 (x0 - ox) * s。
#   可视区右边缘在 layout 里是 ox + W，屏幕宽度是 W * s。
#   施加 translate(dx,dy) scale(1/s)（origin 0 0）后按钮屏幕尺寸仍是 w×h，
#   屏幕左上 = (x0 + dx - ox) * s。要右下角各留 R / B 的边距：
#       dx = ox + W - (w + R) / s - x0
#       dy = oy + H - (h + B) / s - y0
#   s=1、ox=oy=0 时 dx=dy=0，即完全不干预。
VK_CSS = (
    '<style id="vk-follow-style">'
    '.virtual-keyboard-button{'
    'transform-origin:0 0!important;'
    # 上游给 transform 加了 .15s 过渡；跟随 viewport 时每帧都在改，会拖成抖动。
    'transition-property:background-color,border-color!important}'
    '</style>'
)
VK_JS = (
    '<script id="vk-follow-script">(function(){'
    'var Q=".virtual-keyboard-button",seen=0;'
    'function apply(){'
    'var vv=window.visualViewport,el=document.querySelector(Q);'
    'if(!el||!el.offsetWidth){'
    # 只在第一次落空时提示一声，方便在控制台确认选择器是否命中。
    'if(!seen){seen=1;console.warn("[vk-follow] 未找到虚拟键盘按钮，选择器 "+Q)}'
    'return}'
    'if(!seen){seen=1;console.info("[vk-follow] 已接管虚拟键盘按钮")}'
    'var cs=getComputedStyle(el),'
    'R=parseFloat(cs.right),B=parseFloat(cs.bottom);'
    'if(!isFinite(R))R=20;if(!isFinite(B))B=20;'
    'var s=vv&&vv.scale?vv.scale:1,'
    'ox=vv?vv.offsetLeft:0,oy=vv?vv.offsetTop:0,'
    'W=vv?vv.width:document.documentElement.clientWidth,'
    'H=vv?vv.height:document.documentElement.clientHeight,'
    'Lw=document.documentElement.clientWidth,'
    'Lh=document.documentElement.clientHeight,'
    'w=el.offsetWidth,h=el.offsetHeight,'
    'x0=Lw-R-w,y0=Lh-B-h,'
    'dx=ox+W-(w+R)/s-x0,dy=oy+H-(h+B)/s-y0,'
    'v="translate("+dx+"px,"+dy+"px) scale("+(1/s)+")";'
    # 用元素自身的 inline style 做缓存判据：React 重建按钮时新元素没有该样式，
    # 会被重新定位，不会因为数值相同而被跳过。
    'if(el.style.transform===v)return;'
    'el.style.transform=v'
    '}'
    'if(window.visualViewport){'
    'visualViewport.addEventListener("resize",apply);'
    'visualViewport.addEventListener("scroll",apply)}'
    'window.addEventListener("resize",apply);'
    'window.addEventListener("orientationchange",apply);'
    'setInterval(apply,100)'
    '})();</script>'
)

# 二、viewport
html = open(HTML, encoding='utf-8').read()
if html.count('maximum-scale=1,user-scalable=no') != 1:
    sys.exit('[viewport] 锚点期望 1 处，上游写法已变化')
html = html.replace('maximum-scale=1,user-scalable=no', 'maximum-scale=10,user-scalable=yes')

if html.count('</body>') != 1:
    sys.exit(f'[虚拟键盘按钮] </body> 期望 1 处，实际 {html.count("</body>")} 处')
html = html.replace('</body>', VK_CSS + VK_JS + '</body>')
open(HTML, 'w', encoding='utf-8').write(html)

print('Selkies 前端补丁完成')
