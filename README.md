# Chromium Manager

### Chromium 浏览器环境管理系统

轻松管理多个独立浏览器环境，支持指纹伪装、代理配置、Cookie导入导出等。

[核心功能](#-核心功能) • [界面导览](#-界面导览) • [技术架构](#-技术架构) • [安装指南](#-安装指南) • [使用说明](#-使用说明)

---

## ✨ 核心功能

### 🖥️ 多配置管理
- **独立环境** — 每个配置完全隔离
- **分组管理** — 按业务场景自由分组
- **实例管理** — 启动/激活/关闭 Chromium 实例

### 🎭 指纹伪装
- **随机指纹种子** — 每个配置生成随机指纹种子，确保环境独立
- **平台伪装** — 自定义操作系统、浏览器品牌等平台信息
- **硬件参数** — 自定义 CPU 核心数、设备内存、屏幕分辨率
- **多维指纹** — Canvas、WebGL、Audio、Font、ClientRects、GPU

### 🌐 代理管理
- **代理配置** — 集中管理代理服务器，HTTP、HTTPS、SOCKS4、SOCKS5代理协议（支持用户名、密码认证）
- **IP/语言/时区关联** — 代理可绑定 IP 地址、语言、时区和位置
- **自动继承** — 配置可自动继承关联代理的语言、时区和位置

### 🍪 Cookie 管理
- **扩展兼容** — 兼容 Cookie Editor、EditThisCookie 导入导出格式
- **自动配置** — 浏览器自动导入、导出 Cookie
- **导入导出** — 支持手动导入、导出 Cookie

---

## 📸 界面导览

![管理界面](docs/images/image.png)

![浏览器实例](docs/images/image2.png)

---

## 🏗️ 技术架构

| 组件 | 技术 |
|------|------|
| 前端 | Vite + Vue3 + Element Plus |
| 后端 | Go |
| 数据库 | SQLite |
| 浏览器 | Ungoogled Chromium |

---

## 📦 安装指南

### Docker Compose（推荐）

仓库根目录的 [`docker-compose.yml`](docker-compose.yml) 是一份可直接用的配置，使用 Docker
默认的 `bridge` 网络，容器端口通过 `ports` 映射到宿主机：

```yaml
services:
  chromium-manager:
    image: ghcr.io/ldxw/chromium-manager:156.0.8078.79-1-mcp-keys
    container_name: chromium-manager
    network_mode: bridge
    shm_size: 1gb
    environment:
      - PUID=1000
      - PGID=1000
      - TZ=Asia/Shanghai
      - LC_ALL=zh_CN.UTF-8
      # Web 界面 Basic Auth：PASSWORD 留空即不校验；非空时用户名取 CUSTOM_USER（默认 abc）
      - CUSTOM_USER=
      - PASSWORD=
      # MCP 相关，按需开启，详见「🤖 MCP 支持」
      - MCP_ENABLE_DESTRUCTIVE=0
      # - MCP_TOKEN=换成你自己的随机串
      # - MCP_KEYS={"key1":"窗口1","key2":"项目A/窗口1"}
      # - MCP_KEYS_FILE=/config/mcp-keys.json
      # - MCP_GROUP_NAME=mcp
      # - MCP_HTTP_ADDR=0.0.0.0:10102
      # Chromium 沙箱，auto 为默认行为；改成 1 还需配合 seccomp 放开
      # - CHROME_SANDBOX=auto
    ports:
      - 3001:3001
      # 外部 MCP 直连端口，AI 客户端用 http://<宿主机>:10102/mcp
      - 10102:10102
    volumes:
      - ./config:/config
    restart: unless-stopped
```

```bash
docker compose up -d
```

`PUID` / `PGID` 填的是你宿主机用户的 `id -u` / `id -g`，上面写的 `1000` 是多数桌面发行版第一个
普通用户的默认值，**不是镜像默认值**（镜像是 `911`，落在系统账号区间，Linux 上不对应任何真实
用户）。这两个值**必须在第一次 `docker compose up -d` 之前定好**：基础镜像只改 `./config`
挂载点这一层的属主，不递归，之后改值不会修好旧文件。原理与补救办法见
「环境变量 → PUID / PGID 只能设一次」。

`network_mode: bridge` 是显式写出 Docker 的默认网络模式，与 `ports` 映射配合使用。如果你要把
容器接进自定义网络（例如让别的容器按名字访问），把这一行删掉，改用 `networks:` 指定即可，
端口映射依然有效。

`shm_size: 1gb` 建议保留：Chromium 多进程渲染依赖共享内存，低于 1GB 容易出现渲染进程崩溃。

### Docker CLI

```bash
docker run -d \
  --name chromium-manager \
  --shm-size=1gb \
  --network bridge \
  -e PUID=$(id -u) \
  -e PGID=$(id -g) \
  -e TZ=Asia/Shanghai \
  -e LC_ALL=zh_CN.UTF-8 \
  -e CUSTOM_USER= \
  -e PASSWORD= \
  -e MCP_ENABLE_DESTRUCTIVE=0 \
  -e MCP_TOKEN= \
  -e MCP_KEYS= \
  -e MCP_KEYS_FILE= \
  -e MCP_GROUP_NAME=mcp \
  -e MCP_KEY_SLUGS= \
  -e MCP_SHIM_CDP_URL= \
  -e MCP_HTTP_ADDR=0.0.0.0:10102 \
  -p 3001:3001 \
  -p 10102:10102 \
  -v ./config:/config \
  --restart unless-stopped \
  ghcr.io/ldxw/chromium-manager:156.0.8078.79-1-mcp-keys
```

上面 `MCP_TOKEN` / `MCP_KEYS` / `MCP_KEYS_FILE` 留空即不启用鉴权与 key 绑定，
`MCP_GROUP_NAME` 与 `MCP_HTTP_ADDR` 取的就是镜像默认值。要开启时把对应值填进去，
例如 `-e MCP_KEYS='{"key1":"窗口1","key2":"项目A/窗口1","key3":"项目B/窗口1"}'`，
value 支持 `配置名` 与 `分组名/配置名` 两种写法，详见「多 key 绑定」。
`MCP_KEY_SLUGS` / `MCP_SHIM_CDP_URL` 只在多 key 下按 key 分发伪装脚本时才需要，
见「多 key 与浏览器路径脚本」。

镜像不带 `latest`，标签形式为 `<Chromium 版本号>-<变体>`，上例中的 `156.0.8078.79-1`
即二进制来源的 Release 版本。

`-mcp` 后缀是只支持单个 `MCP_TOKEN` 的修复版，`-mcp-keys` 是支持 `MCP_KEYS` 多 key
绑定的版本（同样包含前者的全部修复）；两个标签各自独立发布，构建其中一个不会覆盖另一个。

`-mcp-keys` 的镜像每次构建会同时打两个标签：

```
ghcr.io/ldxw/chromium-manager:156.0.8078.79-1-mcp-keys                 # 固定标签，长期引用这个
ghcr.io/ldxw/chromium-manager:156.0.8078.79-1-mcp-keys-20261110-1542   # 构建时间，UTC+8
```

时间标签的格式是 `<年><月><日>-<时><分>`，北京时间（UTC+8）、24 小时制、精确到分钟，
同一版本反复构建时用来区分具体是哪一次产物。两个标签指向同一份 manifest list，拉哪个都
是一样的镜像；同一分钟内重跑会落到同一个时间标签（后者覆盖前者）。
`-mcp` 标签没有时间标签，只有 `-mcp-keys` 有。

`-e PUID=$(id -u)` 取的是执行这条命令的用户，适合在宿主机上直接跑；在别的机器上准备
compose 文件时，换成目标宿主机 `id -u` / `id -g` 的实际数字。

### 环境变量

容器层面的变量，下表的「默认值」即镜像内的取值；`docker run -e` 或 compose 的
`environment` 可覆盖。环境变量在容器创建时固定，改完必须重建容器
（`docker compose up -d --force-recreate`，或先 `docker rm -f` 再 `docker run`），
`docker restart` 沿用旧值，改了不生效。

| 变量 | 默认值 | 作用 |
| --- | --- | --- |
| `PUID` | `911` | 容器内 `abc` 用户的 UID，也是 `/config` 与新建文件的属主。默认的 `911` 落在系统账号区间（`100-999`），Linux 上不对应任何真实用户；第一次启动前就设成宿主机用户的 `id -u`，见下方「PUID / PGID 只能设一次」 |
| `PGID` | `911` | 同上，对应宿主机的 `id -g`，桌面发行版上通常是 `1000` |
| `TZ` | 空（走 UTC） | 容器时区，影响日志时间与浏览器里的本地时间，如 `Asia/Shanghai` |
| `LC_ALL` | 空（`LANG=en_US.UTF-8`） | 容器 locale，会同步成桌面与 Chromium 的 `LANG` / `LANGUAGE`，镜像内置 `zh_CN.utf8` |
| `CUSTOM_USER` | `abc` | Web 界面 Basic Auth 用户名，仅 `PASSWORD` 非空时生效 |
| `PASSWORD` | 空 | Web 界面 Basic Auth 密码，**留空则不启用登录校验** |
| `TITLE` | `ChromiumManager` | 浏览器内界面的应用名（web app manifest 的 `name` / `short_name`） |
| `DATA_DIR` | `/config/data` | 数据目录：`data.db`、`profiles/`、`screenshots/` 都在下面。直接跑二进制时默认是当前目录的 `data` |
| `NO_DECOR` | `1` | `1` 去掉窗口边框与标题栏，界面铺满整个桌面，看起来像一个独立应用。判据是变量**是否定义**，传空值同样生效；改成不设值即可恢复带标题栏的窗口模式 |
| `SELKIES_MANUAL_WIDTH` | `1920` | 远程桌面的固定宽度，和 `SELKIES_MANUAL_HEIGHT` 一起锁定流分辨率 |
| `SELKIES_MANUAL_HEIGHT` | `1080` | 远程桌面的固定高度。两个值必须一起改，只设一个时服务端会用 `1024` / `768` 补另一个 |

`PUID` / `PGID` / `TZ` / `LC_ALL` / `CUSTOM_USER` / `PASSWORD` / `TITLE` / `NO_DECOR` 由继承的
linuxserver 基础镜像处理。MCP 相关变量（`MCP_TOKEN`、`MCP_KEYS` 等）见
「🤖 MCP 支持 → MCP 环境变量」。

#### 手机端卡在 Waiting for stream...

这是上游 Selkies 前端的缺陷，镜像里做了两处调整，不需要使用者配置。

**主因：解码器配置被浏览器拒绝。** 上游把解码器初始化写死成：

```js
{codec, codedWidth, codedHeight, optimizeForLatency: true,
 hardwareAcceleration: "prefer-software"}
```

真机 WebCodecs 实测（`VideoDecoder.isConfigSupported`）：

| hardwareAcceleration | 1280×720 | 1920×1080 | 2940×4881 |
| --- | --- | --- | --- |
| `"prefer-software"`（上游写法） | false | false | false |
| `"prefer-hardware"` | 视 codec 而定 | 视 codec 而定 | 视 codec 而定 |
| 不传该字段 | true | true | true |

也就是说带 `prefer-software` 时**任何尺寸都过不了校验**，与分辨率无关。前端默认 encoder 是
`x264enc`，它和 `x264enc-striped` 都走分片（stripe）解码器路径：`isConfigSupported` 返回
false 时只 `console.error` 一行 `VNC stripe decoder config not supported for Y=..`、丢掉刚建
好的解码器，下一帧再重建再失败，屏幕始终没有画面，遮罩停在 `Waiting for stream...`。
切到 `jpeg` 时才走主解码器，那条路径失败会进 `Fi()`：降 encoder、写 `_crash_count`、3 秒后
整页 reload。

镜像构建时删掉该字段（`Dockerfile` 里对 `selkies-dashboard/assets/index-*.js` 的 `sed`），
退回实测可行的「不传」路径。电脑之所以能看，是桌面 Chromium 接受了这个提示。

改的是 `selkies-dashboard/assets/`，不是 nginx 的 root `/usr/share/selkies/web/`：后者在容器
启动时由 `init-nginx` 用 `rm -Rf` + `cp -a` 从 `$DASHBOARD`（默认 `selkies-dashboard`）重建，
只改 `web/` 会在启动时被覆盖。

沙箱 A/B 实测（同一台桌面 Chromium，页面里模拟真机行为：带 `hardwareAcceleration` 的配置
一律返回 `supported:false`；手机 UA、视口 980×1627、DPR 3）：

| 容器 | 解码器配置 | probe 结果 | 遮罩 | 日志 |
| --- | --- | --- | --- | --- |
| 未打补丁 | `hardwareAcceleration:"prefer-software"` | false ×2 | 仍在 | `VNC stripe decoder config not supported` |
| 已打补丁 | 不传该字段 | true ×1 | 隐藏 | `Stream started (UI elements hidden).` |

**次因：窗口尺寸频繁变化触发管道重配置。** 手机地址栏收放、键盘弹出都会改 `innerHeight`，
前端每次都发 `r,<宽>x<高>`，服务端每次都整条管道重建；如果某次 `STOP_VIDEO` 之后到来的
尺寸恰好没变，服务端会判定为重复请求直接丢弃（`selkies.py:2384`），采集不再重启。

设了 `SELKIES_MANUAL_*` 之后服务端进入 manual 模式：前端照旧发 `r,`，但 `selkies.py:3369`
会直接忽略（日志 `Client attempted to resize ... Request ignored.`），管道不再被重建；
顺带把手机竖屏的千万像素编码量降到 1920×1080。

两种改法都不需要使用者操作。若手机上仍觉得字小，可以把 `SELKIES_MANUAL_*` 一起调高
（如 `2560`×`1440`），前提是目标手机的解码器能吃下。

#### 手机端捏合缩放

1920×1080 的流等比缩到手机竖屏上只有 390px 宽，内容小到看不清，需要捏合放大。上游有
两处把这个手势堵死，镜像里都做了调整，同样不需要使用者配置：

**一、viewport 禁止缩放。** `selkies-dashboard/index.html` 写死：

```html
<meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1,user-scalable=no">
```

`maximum-scale=1` 会把页面缩放上限钳在 1 倍。镜像里改成 `maximum-scale=10,user-scalable=yes`。
用 `Emulation.setPageScaleFactor` 请求 3 倍实测：未改的页面停在 `visualViewport.scale = 1`，
改后到 `3`。

**二、两指手势被当成滚轮吞掉。** `_handleTouchEvent` 在两指时置 `h = !0`，函数结尾
`h&&this.element.contains(s.target)&&s.preventDefault()` 于是取消浏览器默认手势，两指
换成发滚轮消息（`_triggerMouseWheel`）。只改 viewport 是无效的：实测两指 `touchmove`
仍 5/5 被 `preventDefault`。镜像给结尾加上 `!this._isTwoFingerGesture`，两指不再被拦。

单指操作不受影响，仍由 Selkies 接管（点击、拖动、长按、1→2→1 指的过渡都实测 `prevented`
不变）。

沙箱实测（iPhone UA、390×844、DPR 3，CDP 合成触摸；表中为两指 `touchmove` 被
`preventDefault` 的条数）：

| 状态 | 两指 touchmove | 单指 touchmove | 请求 pageScale 3 |
| --- | --- | --- | --- |
| 上游原样 | 5/5 被拦 | 4/4 被拦 | 停在 1 |
| 只改 viewport | 5/5 被拦 | 4/4 被拦 | 到 3 |
| 两处都改 | 0/5 被拦 | 4/4 被拦 | 到 3 |

CDP 合成的触摸事件不进浏览器 UI 手势流，所以 `visualViewport.scale` 不会因合成捏合而变；
上表最后一列用 `Emulation.setPageScaleFactor` 测的是 viewport 钳制本身。真机上两指手势
在浏览器层就能生效。

#### 给外部自动化模块提供浏览器路径

有些自动化模块（手机上的刷抖音一类）只接受一个「浏览器路径」，内部拿它当
puppeteer 的 `executablePath` 用：自己 spawn 那个文件，然后从它的 stderr 里等
`DevTools listening on ws://...` 这一行来接管浏览器。浏览器跑在容器里，本机没有这个
文件，于是镜像里放了一个同名的伪装脚本 `/usr/local/bin/chrome-mcp`。

**它不启动浏览器，只做地址转交。** 被调用时向 manager 的 `/cdp` 查地址，把拿到的
websocket 地址按 Chrome 的原格式打到 stderr，然后守着不退（调用方通常以「子进程退出
= 浏览器崩溃」处理）：

```bash
$ MCP_CDP_HTTP=http://192.168.1.10:10102 /usr/local/bin/chrome-mcp --headless=new
DevTools listening on ws://192.168.1.10:10102/cdp/UkLWZg9D/devtools/browser/6f3c...?token=...
```

`--version` 会回一行 `Chromium 156.0.8078.79`，部分模块启动前会探测它。

**必须用明文端口。** puppeteer 解析这一行用的是固定正则
`/^DevTools listening on (ws:\/\/.*)$/`，只认 `ws://`。`MCP_CDP_HTTP` 填 `https://`
时 manager 会把地址改写成 `wss://`，puppeteer 拿到也匹配不上，最后报
`Timed out after 25000 ms while waiting for the WS endpoint URL to appear in stdout`。
所以镜像的 3001（HTTPS）那条**不能**喂给它：`/chrome-mcp` 从 3001 下载没问题，但
`MCP_CDP_HTTP` 要指回明文端口——10102，或本机映射出来的 3000。脚本发现拿到 `wss://`
会额外打一行提示，不会静默等到超时。

**为什么不是直接连 9222。** Chromium 的 DevTools 只监听 `127.0.0.1`，实测在 156 上
传 `--remote-debugging-address=0.0.0.0` 不起作用——headless 与 headful 下 `ss -ltn`
都仍是 `127.0.0.1:PORT`，从容器网卡地址连不上。所以地址要由 manager 转出来：
后端把 `/cdp/<配置 _id>/...` 反代到该实例的随机调试端口，`/json/version` 响应里的
`webSocketDebuggerUrl` 也被改写成这个对外可达的路径。好处是**不用给实例配任何启动
参数**（默认的 `--remote-debugging-port=0` 随机端口即可）、不用占宿主机端口、也不用
为多开分配端口。

**取这个文件。** 手机侧拿不到容器文件系统，走 HTTP 下载（3000 与 3001 都有，`/mcp` 的
同一条 nginx 反代之外单独挂了这个路由）：

```bash
curl -fsS http://<宿主机>:3000/chrome-mcp -o chrome-mcp && chmod +x chrome-mcp
```

也可以从 10102 外部 MCP 端口取：
`curl -fsS http://<宿主机>:10102/chrome-mcp -o chrome-mcp && chmod +x chrome-mcp`。
容器里则直接 `docker cp <容器名>:/usr/local/bin/chrome-mcp .`，同样需要 `chmod +x`。

**按 key 取预填版（多 key 时推荐）。** 设了 `MCP_KEYS` 后，带自己的凭据请求
`/chrome-mcp-<slug>`，拿到的脚本已经把 `MCP_CDP_TOKEN` 与 `MCP_CDP_HTTP` 的默认值填好，
下载下来 `chmod +x` 就能直接用，不必再给模块配任何环境变量：

```bash
# 文件名由 MCP_KEY_SLUGS 决定；没设时就是 token 本身
curl -fsS -H 'Authorization: Bearer key1' \
  http://<宿主机>:10102/chrome-mcp-1 -o chrome-mcp-1 && chmod +x chrome-mcp-1
```

`?token=<key>` 与 Bearer 等价（浏览器里点链接下载时加不了请求头）：

```bash
curl -fsS 'http://<宿主机>:10102/chrome-mcp-1?token=key1' -o chrome-mcp-1 && chmod +x chrome-mcp-1
```

| 请求 | 结果 |
| --- | --- |
| `/chrome-mcp`，不带凭据 | 通用脚本，文件名 `chrome-mcp`，内容与镜像里的一模一样 |
| `/chrome-mcp`，带凭据 | 该 key 的预填版，文件名 `chrome-mcp-<slug>` |
| `/chrome-mcp-<slug>`，带正确凭据 | 预填版，`Content-Disposition` 里的文件名是 `chrome-mcp-<slug>` |
| `/chrome-mcp-<slug>`，不带凭据 | 401（带 `WWW-Authenticate: Bearer`） |
| `/chrome-mcp-<slug>`，凭据是别人的 | 403，只说明「该凭据只能下载自己的文件」 |
| `/chrome-mcp-<slug>`，slug 不存在 | 403（先校验凭据再判定，所以不会因为「401 vs 403」枚举出哪些 slug 存在） |
| 单 `MCP_TOKEN` 模式下带任何 slug | 404，该模式没有按 key 区分的文件 |

预填只写入 `CDP_TOKEN` 与 `CDP_HTTP` 两个默认值，`CDP_PROFILE` 仍留空——多 key 时它本来
就该留空，`/cdp` 会落到该 key 绑定的配置。环境变量优先级高于脚本里的默认值，所以模块
自己设了 `MCP_CDP_HTTP` / `MCP_CDP_TOKEN` 时仍以模块为准。

`CDP_HTTP` 的取值顺序：`MCP_SHIM_CDP_URL`（显式指定，原样使用）→ `MCP_HTTP_ADDR` 里的
端口拼到本次请求的 Host 上 → `http://<请求 Host>`。一律给明文 `http://`，因为 puppeteer
只认 `ws://`；经 3001（HTTPS）下载时也会被改写成明文端口。

**`chmod +x` 不能省。** HTTP 响应不携带权限位，`curl -o` 建出来的文件权限由 umask 决定
（通常是 `0644` / `0664`），没有执行位。模块拿这个路径去 `spawn` 时内核直接返回
`EACCES`，表现为「浏览器起不来」而不是脚本报错。实测未 `chmod` 时
`./chrome-mcp --version` 是 `Permission denied`（退出码 126），加完立刻正常。

Android 上另有一个坑：`/sdcard`（`/storage/emulated/0`）是 FUSE 挂载且带 `noexec`，
放在那里的文件即使权限位看着是 `0755` 也执行不了（`chmod` 可能直接报
`Operation not permitted`，也可能改了但挂载选项仍拦着）。要放到应用私有目录
（如 Termux 的 `$PREFIX/bin`，或 Aurabot 数据目录下能执行的路径）。

**配置伪装脚本。** 建议在模块里把这几项一起设上：

| 环境变量 | 默认值 | 作用 |
| --- | --- | --- |
| `MCP_CDP_HTTP` | `http://127.0.0.1:10102` | manager 的地址。脚本据此查 `/cdp`。手机/局域网填 `http://<宿主机>:10102` |
| `MCP_CDP_PROFILE` | 空 | 配置 `_id`。留空时取唯一运行中的实例；同时开着多个会报错并列出候选 `_id`。容器设了 `MCP_KEYS` 时留空即可，会自动落到这个 key 绑定的配置 |
| `MCP_CDP_TOKEN` | 空 | manager 的凭据：设了 `MCP_KEYS` 就填分配给你的那个 key，否则填 `MCP_TOKEN`。必填，脚本会把它放进 URL 的 `?token=` |
| `MCP_CDP_WS` | 空 | 直接给出 ws 地址时跳过 HTTP 查询，故障排查用 |
| `MCP_CDP_TIMEOUT` | `20` | 等待端点就绪的秒数。puppeteer 自己 30s 就放弃，别超过它 |
| `MCP_CDP_VERSION` | `156.0.8078.79` | `--version` 的返回值 |

手机上通常写成（单 key）：

```
MCP_CDP_HTTP=http://<宿主机>:10102
MCP_CDP_TOKEN=<和容器里的 MCP_TOKEN 一致>
```

容器用 `MCP_KEYS` 时，每个模块填自己那个 key，`MCP_CDP_PROFILE` 不用填：

```
MCP_CDP_HTTP=http://<宿主机>:10102
MCP_CDP_TOKEN=<分配给你的 key，如 key1>
```

**只要开一个实例就不用填 `MCP_CDP_PROFILE`。** `/cdp` 这条路由在恰好一个实例运行时
直接回它的 `/json/version`；0 个返回 503，多个返回 409 并把各自的 `_id` 与名字列出来。
要固定连某个配置时填 `MCP_CDP_PROFILE=<_id>`，它就只走 `/cdp/<_id>/json/version`。
容器设了 `MCP_KEYS` 时这条判断不适用：`/cdp` 直接落到该 key 自己的配置，不受「同时跑着
几个实例」影响，填别人的 `_id` 会得到 403。

**安全边界。** 这条路径和 Web 界面/MCP 共用同一套凭据（`MCP_KEYS` 优先，否则
`MCP_TOKEN`）：设了就要求 Bearer 或 `?token=`，没设则任何能连到该端口的人都能
**完全控制**运行中的实例——读改 Cookie、在已登录站点上执行任意 JS、导出会话。浏览器级
的 CDP 权限和「登录了账号的那台浏览器」完全等价，所以：

- 公网暴露时务必设 `MCP_TOKEN` 或 `MCP_KEYS`，并优先只映射 `10102` 这一条（或经 3001 的 HTTPS）。
- 只在本机用时把 `MCP_HTTP_ADDR` 设成 `127.0.0.1:10102`，手机侧先建隧道：

  ```bash
  ssh -N -L 10102:127.0.0.1:10102 <宿主机>
  ```

  手机上把 `MCP_CDP_HTTP` 指向 `http://127.0.0.1:10102` 即可。
- 不要把凭据留给默认空值后又把端口开到公网；token 会出现在 URL 里，
  可能被日志、历史记录记下，属可控但需知晓的风险。

**模块不支持设置环境变量时**，直接改下载到的文件里的默认值（单 key 写成你的
`MCP_TOKEN`，多 key 写成分配给你的那个 key）：

```bash
sed -i 's|http://127.0.0.1:10102|http://192.168.1.10:10102|' chrome-mcp
sed -i 's|CDP_TOKEN="${MCP_CDP_TOKEN:-}"|CDP_TOKEN="你的 MCP_TOKEN"|' chrome-mcp
```

#### PUID / PGID 只能设一次

`PUID` / `PGID` 决定容器内 `abc` 用户的 UID / GID，也是挂载的 `./config` 的属主。基础镜像
启动时只把**挂载点这一层**改过去，不会递归处理里面的文件：

```bash
lsiown abc:abc /config      # 等价于 find /config -maxdepth 0 -exec chown ...
```

所以：

- `./config` 本身会变成你设的 UID / GID，之后程序新建的文件（`data.db`、`profiles/` 等）
  也归你；
- `./config` 里**启动前就有的旧文件**保持原属主不动。

这带来两个后果，都和「改小 / 改回默认」有关：

1. **先按 `PUID=1000` 跑过，再改回默认 `911`，程序会写不了库。** `data.db` 属主是 1000、
   权限 644，`data/` 目录也是 1000 且不是 777，`abc`（911）既改不了文件也建不了新文件，
   日志里出现 `Permission denied`，管理界面读不到数据。
2. **反过来，从 `911` 改成 `1000` 同样不会自动修好旧文件**，因为它们不在递归范围内。

`PUID` 要在**第一次 `docker compose up` 之前**就按宿主机用户定好：

```bash
id -u   # 填给 PUID
id -g   # 填给 PGID
```

已经设错、需要换归属时，在宿主机上手动递归改一次，之后保持这个值不变：

```bash
sudo chown -R "$(id -u):$(id -g)" ./config
```

### Web 界面登录

镜像继承 linuxserver 的基础镜像，Web 界面（3000 / 3001）走 HTTP Basic Auth，由两个环境变量控制：

| 变量 | 作用 |
| --- | --- |
| `PASSWORD` | Basic Auth 密码，**留空则不启用登录校验** |
| `CUSTOM_USER` | Basic Auth 用户名，默认 `abc`，仅在 `PASSWORD` 非空时起作用 |

是否启用鉴权只由 `PASSWORD` 决定：`PASSWORD` 非空时，初始化脚本会生成
`/etc/nginx/.htpasswd`（用户名取 `CUSTOM_USER`，为空则用 `abc`）并打开 nginx 配置里的
`auth_basic`，浏览器访问 `https://<宿主机>:3001/` 时弹出登录框；`PASSWORD` 留空则整站
不做校验，此时单独设置 `CUSTOM_USER` 不起任何作用。

`/mcp` 端点在 nginx 里单独 `auth_basic off;`，不使用这两个变量，只用 `MCP_TOKEN` 或
`MCP_KEYS` 的 Bearer，详见「🤖 MCP 支持」。

如果容器暴露在公网，建议一定要设 `PASSWORD`（用户名可保持默认 `abc`），否则任何能连到
3001 的人都可以直接操作管理界面。

镜像默认发布到 GHCR（`ghcr.io/<owner>/chromium-manager`），也可以推阿里云镜像仓库或
Docker Hub，构建由 `.github/workflows/docker-image.yml` 完成：

- 手动触发，可选填 `release_tag`（二进制来源）与 `image_tag`（镜像标签）；
  两者留空时，前者取仓库最新的正式 Release，后者跟随前者。
- `push_ghcr` **默认勾选**，取消后本次完全不碰 GHCR，可以只推阿里云或 Docker Hub。
  GHCR 鉴权用仓库自带的 `GITHUB_TOKEN`，不需要额外 secret。
- 勾选 `push_aliyun` 后推送一份到
  `<ALIYUN_REGISTRY>/<ALIYUN_NAMESPACE>/chromium-manager:<版本号>-mcp-keys`，
  需要在仓库 Secrets 里配置 `ALIYUN_REGISTRY`、`ALIYUN_NAMESPACE`、
  `ALIYUN_USERNAME`、`ALIYUN_PASSWORD`；缺任意一个会在构建前直接失败。
- 勾选 `push_dockerhub` 后推送一份到
  `<DOCKERHUB_USERNAME>/chromium-manager:<版本号>-mcp-keys`，需要在仓库 Secrets 里配置
  `DOCKERHUB_USERNAME` 与 `DOCKERHUB_TOKEN`；缺任意一个会在构建前直接失败。
- 三个开关可以任意组合，至少勾选一个；全不勾选会在解析参数阶段直接失败。
- 构建 `linux/amd64` 与 `linux/arm64` 两个架构，标签固定带 `-mcp-keys` 后缀。
- 每次构建同时推两个标签：固定的 `<版本号>-mcp-keys` 与带 UTC+8 构建时间的
  `<版本号>-mcp-keys-<YYYYMMDD>-<HHMM>`。两个标签共享同一个 manifest list，
  多打一个标签不额外增加构建或上传成本；时间按 `date -u -d '+8 hours' +%Y%m%d-%H%M` 取，
  不依赖 runner 上的 tzdata（`TZ=Asia/Shanghai` 在缺 tzdata 时会静默退回 UTC）。
- 构建失败会自动重试最多 3 次，用于绕过拉取基础镜像时的偶发 429。
- GHCR 与 Docker Hub 保留 buildx 默认的 provenance attestation。勾选 `push_aliyun` 时，
  阿里云那份会**单独再推一次**，关闭 provenance 与 SBOM，并固定输出 Docker schema2
  （`--output type=image,oci-mediatypes=false`）：
  - 阿里云 ACR 不认 `application/vnd.oci.empty.v1+json` 这个 attestation manifest 类型，
    会拒绝整次推送；
  - 关掉 attestation 后 buildx 仍可能输出 OCI image index，而 ACR 按 Docker v2 仓库使用，
    收下后控制台与拉取都解析不出内容（表现为镜像“空的”）。固定 schema2 后不再受
    runner 上 buildkit 版本的影响。
  两次推送复用同一个 buildkit 层缓存，第二次只重新组 manifest 并上传，不重复编译。
  阿里云那份的 manifest list 因此只有 amd64 / arm64 两条真实镜像，无 attestation。
- 每组推送成功后都会**回读校验**（`.github/scripts/verify-image.py`）：确认 tag 指向多架构
  索引、平台恰为 `linux/amd64` / `linux/arm64`，并逐个确认子 manifest 引用的 config 与
  全部 layer blob 在仓库里真的存在。registry 只校验 manifest 本身，不校验它引用的 blob，
  所以 buildx 报成功不等于镜像可用；校验不过按该组失败处理，CI 会直接红掉。

### 通过以下网址访问应用

* https://yourhost:3001/

---

## 📖 使用说明

### 创建浏览器配置

1. 在左侧面板创建**分组**
2. 在右侧面板添加新**配置**
3. 配置指纹参数
4. 关联代理（可选）
5. **启动**打开浏览器，**激活**切换到前台，**关闭**关闭浏览器

### 代理配置

1. 进入代理管理页面
2. 添加代理服务器
3. 在配置中选择代理

### Cookie 管理

- **手动导入**: 导入 JSON 格式的 Cookie
- **手动导出**: 导出当前配置的 Cookie

---

## 🤖 MCP 支持

程序内置 MCP 服务器，可被任意 MCP 客户端（Claude Desktop、Claude Code、Cursor 等）驱动，
直接管理配置、启动浏览器并在页面里导航、点击、填表、截图。

### 传输方式

| 方式 | 入口 | 说明 |
| --- | --- | --- |
| stdio | `ChromiumManager --mcp-stdio` | 由客户端拉起子进程，stdout 是协议通道，日志走 stderr |
| Streamable HTTP（内部） | `POST http://127.0.0.1:10101/mcp` | 容器内回环，供 nginx 反代和浏览器内界面使用 |
| Streamable HTTP（外部） | `POST http://0.0.0.0:10102/mcp` | 独立监听，容器外 AI 客户端可直连，不经 nginx |

HTTP 传输在容器内只监听 `127.0.0.1:10101`，镜像里的 nginx 已在两个 server 块（容器内
3000 = HTTP、3001 = HTTPS）各加了一条 `location /mcp` 反代到该端口，所以**容器外可以直接
用映射出来的端口访问 `/mcp`**：

```bash
curl -X POST http://<宿主机>:3000/mcp -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <你的 MCP_TOKEN>' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

curl -k -X POST https://<宿主机>:3001/mcp -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <你的 MCP_TOKEN>' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

`/mcp` 是容器内的绝对路径，设了 `SUBFOLDER`（子路径反代）时也不带前缀。反代只放行了
`POST`，`GET` / `DELETE` 返回 405，请求体上限 8MB（超过返回 413）。

`/mcp` 这一条 location 关了 nginx 的 Basic Auth（`auth_basic off;`）。HTTP 的
`Authorization` 头一个请求只能带一种凭据，Basic 与 MCP 自己的 Bearer 同时开启时任何请求
都会卡在某一层，因此 `/mcp` 只认 Bearer；Web 界面仍走由 `PASSWORD` 启用的 Basic Auth
（用户名 `CUSTOM_USER`），两者互不影响。

因为端点现在可以从容器外触达，**强烈建议设置 `MCP_TOKEN` 或 `MCP_KEYS`**；两个都不设时任何
能连到该端口的人都能调用全部已启用的工具。要把端点交给外部使用方，用 `MCP_KEYS` 给每人分配
一个 key 和一个配置，见「多 key 绑定」。

### 外部直连端口

除 nginx 反代之外，程序还会另起一个独立的 MCP 监听，默认 `0.0.0.0:10102`，挂 `/mcp`、
`/healthz`、`/chrome-mcp`（含预填版 `/chrome-mcp-<slug>`）、`/cdp` 这几条路由，不暴露管理
界面，也不经过 Basic Auth：

```bash
docker run ... -p 10102:10102 ...

curl -X POST http://<宿主机>:10102/mcp -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <你的 MCP_TOKEN>' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

curl http://<宿主机>:10102/healthz   # 返回 ok，可用于探活
curl -fsS http://<宿主机>:10102/chrome-mcp -o chrome-mcp && chmod +x chrome-mcp
```

地址由 `MCP_HTTP_ADDR` 控制：改成 `127.0.0.1:10102` 可退回仅本机，设为 `off` 完全关闭。
鉴权与 nginx 反代共用同一套：设了 `MCP_KEYS` 时按 key 查表，否则读 `MCP_TOKEN`，两个都为空
时不校验。要把端点交给外部使用方，见「多 key 绑定」。

### CDP 反向代理

实例的 DevTools 端口只绑 `127.0.0.1`（`--remote-debugging-address` 在 156 上无效），
容器外拿不到。manager 因此在 `/cdp` 下做一层反代，把各实例的随机端口转出来：

| 路径 | 作用 |
| --- | --- |
| `/cdp` | 恰好一个实例在跑时回它的 `/json/version`；0 个返回 503，多个返回 409 并列出候选 `_id` |
| `/cdp/<配置 _id>/json/version` | 该实例的浏览器信息，`webSocketDebuggerUrl` 已改写成本代理地址 |
| `/cdp/<配置 _id>/json/list` | 该实例的 target 列表，同样改写 |
| `/cdp/<配置 _id>/devtools/...` | CDP 的 websocket 通道，可建页面会话 |

```bash
TK=<你的 MCP_TOKEN>
curl -sS "http://<宿主机>:10102/cdp?token=$TK"          # 唯一实例的 /json/version
curl -sS "http://<宿主机>:10102/cdp/<_id>/json/list?token=$TK"
```

鉴权和 `/mcp` 一样（`MCP_KEYS` 优先，否则 `MCP_TOKEN`），但**额外接受 `?token=`**：
puppeteer 之类建立 CDP 连接时没法加自定义请求头，凭据只能放进 URL。返回的
`webSocketDebuggerUrl` 已经带上同一个 token，客户端拿到直接连即可。经 nginx（3000/3001）
访问时同样可用，那两条 `location /cdp` 都关了 Basic Auth 并放行了 websocket 升级。

设了 `MCP_KEYS` 时 `/cdp` 也按绑定隔离：`/cdp` 直接落到该 key 自己的配置，不必填 `_id`；
显式填别人的 `_id` 返回 403。所以每个使用方用自己那个 key 配 `MCP_CDP_TOKEN` 即可，
`MCP_CDP_PROFILE` 可以留空。详见「多 key 绑定」。

### 客户端配置

stdio：

```json
{
  "mcpServers": {
    "chromium-manager": {
      "command": "/path/to/ChromiumManager",
      "args": ["--mcp-stdio"],
      "env": { "DATA_DIR": "/path/to/data" }
    }
  }
}
```

Streamable HTTP（先正常运行程序）：

```json
{
  "mcpServers": {
    "chromium-manager": {
      "type": "http",
      "url": "http://127.0.0.1:10101/mcp"
    }
  }
}
```

### MCP 环境变量

| 变量 | 默认值 | 作用 |
| --- | --- | --- |
| `MCP_ENABLE_DESTRUCTIVE` | `0` | 设为 `1` 才注册删除类工具（`delete_profile` / `delete_proxy` / `delete_group` / `stop_all_profiles`） |
| `MCP_TOKEN` | 空 | 仅 HTTP 传输生效；设置后要求 `Authorization: Bearer <token>`，未设置不校验。反代与外部端口都在容器外可触达，建议始终设置 |
| `MCP_KEYS` | 空 | 多 key 绑定，JSON 对象，value 写 `配置名` 或 `分组名/配置名`，见「多 key 绑定」。设置后优先于 `MCP_TOKEN` |
| `MCP_KEYS_FILE` | 空 | 从文件读取上面那份 JSON；与 `MCP_KEYS` 同时设置时以 `MCP_KEYS` 为准 |
| `MCP_GROUP_NAME` | `mcp` | 省写分组时用的默认分组名；value 里带 `分组名/` 的条目不受它影响 |
| `MCP_KEY_SLUGS` | 空 | `/chrome-mcp-<slug>` 的文件名后缀，JSON 对象，形如 `{"key1":"1","key2":"2"}`。只影响下载文件名，不参与鉴权；未配置的 token 用 token 本身。需与 `MCP_KEYS` 一起用 |
| `MCP_SHIM_CDP_URL` | 空 | 预填版脚本里写死的 `CDP_HTTP`（manager 地址）。不设时按请求 Host 加 `MCP_HTTP_ADDR` 的端口推断；必须给明文 `http://` |
| `MCP_HTTP_ADDR` | `0.0.0.0:10102` | 外部直连监听地址；设为 `127.0.0.1:10102` 仅本机可用，设为 `off` 关闭。空值按默认处理，`0` / `false` 与 `off` 等价 |
| `MCP_SHIM_PATH` | `/usr/local/bin/chrome-mcp` | `/chrome-mcp` 要下发的脚本在本机的位置。镜像里已放在默认路径，只在自建镜像改了位置时才需要设 |
| `CHROME_SANDBOX` | `auto` | 控制 Chromium 沙箱，见下方说明 |

需要 HTTP 传输鉴权时，在 `docker run` 里再加 `-e MCP_TOKEN=<随机串>`（compose 中放开被注释的那行），
客户端请求必须带 `Authorization: Bearer <随机串>`，否则返回 401。只想给外部使用方分配独立配置时，
用 `MCP_KEYS` 代替 `MCP_TOKEN`，见下一节。

### 多 key 绑定

把 MCP 端点交给外部使用方（不同人、不同 AI 客户端）时，用 `MCP_KEYS` 给每个使用方分配一个 key
和它专属的浏览器配置。每个 key 只能看到、只能操作自己那个配置：

```bash
docker run -d --name chromium-manager \
  -e MCP_KEYS='{"key1":"窗口1","key2":"窗口2"}' \
  -e MCP_GROUP_NAME=mcp \
  -p 10102:10102 \
  ghcr.io/ldxw/chromium-manager:156.0.8078.79-1-mcp-keys
```

上面的 value 只有配置名，会落到 `MCP_GROUP_NAME`（默认 `mcp`）这一个分组里。要让不同的使用方
各自拥有独立分组，value 写成 `分组名/配置名`：

```bash
-e MCP_KEYS='{"key1":"项目A/窗口1","key2":"项目A/窗口2","key3":"项目B/窗口1"}'
```

这样 `key1` / `key2` 的两个配置在「项目A」下，`key3` 的在「项目B」下，三个 key 互相看不到。
两种写法可以混用，不带 `/` 的仍然落到默认分组：

```bash
-e MCP_GROUP_NAME=默认组 \
-e MCP_KEYS='{"key1":"窗口1","key2":"项目A/窗口1","key3":"项目B/窗口1"}'
# → 默认组/窗口1、项目A/窗口1、项目B/窗口1
```

`/` 只认第一个作分隔符，配置名本身不能含 `/`；分组名与配置名两端的空白会被去掉。

也可以用文件，适合 value 里含特殊字符或 key 较多的情况（`MCP_KEYS` 与 `MCP_KEYS_FILE` 同时设置时
前者优先）：

```bash
-e MCP_KEYS_FILE=/config/mcp-keys.json
```

```json
{"key1": "窗口1", "key2": "项目A/窗口1"}
```

启动时程序会：

1. 按需创建 value 里出现的每个分组（`MCP_GROUP_NAME` 只在省写分组时才会用到），已经存在的直接复用；
2. 在各分组下按配置名找配置，没有就建一个（自动分配随机指纹种子），**只创建不启动**；
3. 建立 key → 配置 的内存映射，日志里只打印 key 前 4 位。

客户端照常 `Authorization: Bearer key1` 即可。绑定生效后：

| 行为 | 说明 |
| --- | --- |
| `id` 可省略 | `get_profile` / `launch_profile` / `stop_profile` / 6 个浏览器工具缺省即操作绑定配置 |
| 传别人的 `id` | 直接拒绝：`当前 key 已绑定到配置「项目A/窗口1」，不能操作其他配置` |
| `list_profiles` / `list_running` / `list_groups` | 只返回自己那条（`list_running` 不含别人的 pid 与 DevTools 地址） |
| `list_proxies` | 不返回 `url`（里面带账号密码），只给 ip / 语言 / 时区 / 位置 |
| `add_profile` / `update_profile` / 删除类工具 | 不注册，`tools/list` 看不到，调用返回 unknown tool |
| `screenshot` | 忽略 `path`，直接返回 MCP `image` 内容（PNG base64），不写文件 |
| `navigate` | 只允许 `http://` 与 `https://`，`file://` / `chrome://` / `data:` / `javascript:` 一律拒绝 |
| `/cdp` | 自动落到自己那个配置；带别人的 `_id` 返回 403 |

`file://` 之所以必须堵掉：容器里 `/config/data/data.db` 是整份 SQLite 库，`file://` 加 `read_page`
就能把所有配置的 cookie 与代理凭据读出来。

绑定配置如果在 Web 界面里被改名、删除，或者被挪到别的分组，该 key 的请求会返回
`绑定的配置已不存在、被重命名或被挪到别的分组，请重启容器以按 MCP_KEYS 重新创建`，
不会自动重建——自动重建会让外部使用方悄悄换到一个新环境。重启容器后启动逻辑会重新
按名称校正。

两个 key 绑定同一个「分组 + 配置」、key 为空、分组名或配置名为空、配置名里出现第二个 `/`，
都会在启动时打印具体原因并让进程退出，不带着半套绑定对外服务。stdin 传输（`--mcp-stdio`）
不读 `MCP_KEYS`，行为与以前一样。

#### 多 key 与浏览器路径脚本

`MCP_KEYS` 和「给外部自动化模块提供浏览器路径」是配套的：把 key 发给不同使用方，各自的
`MCP_CDP_TOKEN` 填自己那个 key、`MCP_CDP_PROFILE` 留空，脚本查 `/cdp` 时就会落到自己绑定
的配置，不需要知道任何 `_id`：

```bash
# 容器
-e MCP_KEYS='{"key1":"项目A/窗口1","key2":"项目A/窗口2"}' \
-e MCP_KEY_SLUGS='{"key1":"1","key2":"2"}'

# 使用方 1：下载 chrome-mcp-1，不用再配环境变量
curl -fsS 'http://<宿主机>:10102/chrome-mcp-1?token=key1' -o chrome-mcp-1 && chmod +x chrome-mcp-1

# 使用方 2
curl -fsS 'http://<宿主机>:10102/chrome-mcp-2?token=key2' -o chrome-mcp-2 && chmod +x chrome-mcp-2
```

不带凭据时 `/chrome-mcp` 仍返回通用脚本（脚本里没有任何密钥），任何能连到端口的人都能
取到；预填版则要求请求自己带上该 key。两条路径的真正访问控制都在 `/cdp` 那一步的 key
校验上——取到通用脚本但没有 key 的人连不上任何实例。所以把 10102 开到公网时，
`MCP_KEYS` / `MCP_TOKEN` 必须设。

`MCP_KEY_SLUGS` 只决定文件名后缀，不参与鉴权：没给某个 token 配 slug 时用 token 本身
（非 `[A-Za-z0-9_.-]` 的字符换成 `-`，首尾的 `-` / `.` 去掉），所以不设这个变量也能正常下载。
两个 token 算出同一个 slug 时启动直接失败，不会让两个 key 抢同一个文件。

`MCP_KEY_SLUGS` 的 key 必须与 `MCP_KEYS` 里的 key 逐字一致，value 是你要的后缀：

| 写法 | 结果 |
| --- | --- |
| 不设 `MCP_KEY_SLUGS` | 文件名回退成 token：`chrome-mcp-key1` |
| `{"key1":"key1"}` | 与不设相同，`chrome-mcp-key1`。值写成 token 没有额外意义 |
| `{"key1":"1"}` | `chrome-mcp-1` |
| `{"key1":"1","key2":"1"}` | 启动失败：两个 key 抢同一个文件名 |
| `{"kye1":"1"}`（key 拼错） | 启动警告并忽略这条，文件名仍是 `chrome-mcp-key1`；`/chrome-mcp-1` 返回 403 |
| 只设 `MCP_KEY_SLUGS` 不设 `MCP_KEYS` | 启动警告，后缀不生效（单 token 模式只有 `/chrome-mcp`） |

后缀只出现在路径与 `Content-Disposition` 里，不影响鉴权——访问 `/chrome-mcp-1` 时校验的仍
是请求自己带的 key。所以**知道别人的后缀也拿不到别人的文件**，只会得到 403。

#### 按 key 分发脚本：完整配置

这一节把 `MCP_KEYS`、`MCP_KEY_SLUGS`、`MCP_SHIM_CDP_URL` 三者的关系讲完。只想让 AI 客户端
连上 `/mcp` 的，读「多 key 绑定」就够了；要让手机/自动化模块拿到**下载即用**的浏览器路径，
按下面配。

**一、服务端（容器）**

```yaml
environment:
  - MCP_KEYS={"key1":"项目A/窗口1","key2":"项目A/窗口2"}
  - MCP_KEY_SLUGS={"key1":"1","key2":"2"}
  # 可选：脚本里写死的 manager 地址。不设时按请求 Host + MCP_HTTP_ADDR 的端口推断
  - MCP_SHIM_CDP_URL=http://192.168.1.10:10102
```

三个人各管一件事，互不替代：

| 变量 | 决定什么 | 不设时 |
| --- | --- | --- |
| `MCP_KEYS` | 谁是合法凭据、各自操作哪个配置 | 退回单一 `MCP_TOKEN` 模式，不生成预填版 |
| `MCP_KEY_SLUGS` | 下载文件名里的后缀（也就是 URL 里的 `<slug>`） | 用 token 本身当后缀 |
| `MCP_SHIM_CDP_URL` | 预填进脚本的 `CDP_HTTP`（manager 地址） | 按请求 Host + `MCP_HTTP_ADDR` 端口推断 |

**二、使用方（手机/自动化模块）**

每个使用方只需一条命令，不用再配任何环境变量：

```bash
curl -fsS 'http://192.168.1.10:10102/chrome-mcp-1?token=key1' -o chrome-mcp-1
chmod +x chrome-mcp-1
```

`chmod +x` 必须做：HTTP 响应不带权限位，`curl -o` 建出来是 `0644`，直接 spawn 会 `EACCES`。
Android 上还要放在可执行目录（Termux 的 `$PREFIX/bin` 等），`/sdcard` 是 `noexec` 挂载。

**三、`CDP_HTTP` 是怎么定下来的**

按顺序取第一个可用的：

1. `MCP_SHIM_CDP_URL`（原样使用，去掉末尾 `/`）；
2. `MCP_HTTP_ADDR` 里的端口，配到**本次请求的 Host** 上——所以从手机访问
   `http://192.168.1.10:10102/chrome-mcp-1` 拿到的就是 `http://192.168.1.10:10102`；
3. `MCP_HTTP_ADDR=off` 时退回**请求自己的端口**（例如经 3000 下载就给 `http://<host>:3000`）；
4. 请求也没带端口时给 `http://<host>`。

一律是明文 `http://`，哪怕你从 3001（HTTPS）下载——puppeteer 的正则
`/^DevTools listening on (ws:\/\/.*)$/` 只认 `ws://`，给 `wss://` 会一直等到 25s 超时。

实测三种取值：

| 配置 | 请求 | 预填的 `CDP_HTTP` |
| --- | --- | --- |
| `MCP_HTTP_ADDR=127.0.0.1:10132` | `http://127.0.0.1:10132/...` | `http://127.0.0.1:10132` |
| 同上 + `MCP_SHIM_CDP_URL=http://lan.example:9999/` | 同上 | `http://lan.example:9999` |
| `MCP_HTTP_ADDR=off` | `http://127.0.0.1:10101/...` | `http://127.0.0.1:10101` |

`MCP_SHIM_CDP_URL` 的典型用途：容器放在反代/隧道后面，外面看到的主机名与请求 Host 不一致
（`https://cm.example.com` → 容器 10102），这时直接写 `MCP_SHIM_CDP_URL=http://<内网可达地址>:10102`。

**四、后缀的合法字符**

非 `[A-Za-z0-9_.-]` 的字符逐个换成 `-`，再去掉首尾的 `-` 与 `.`，结果为空时用 `default`。
所以 `{"key1":"项目1"}` 会得到 `chrome-mcp---1`（两个汉字各换一个 `-`），不推荐；用
数字或短横线最省事。后缀同时也是 `Content-Disposition` 里的文件名，不会改变鉴权。

**五、常见配错与表现**

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| 启动日志 `WARNING: MCP_KEY_SLUGS 里的 **** 不在 MCP_KEYS 中` | `MCP_KEY_SLUGS` 的 key 拼错（区分大小写、不能带空格） | 改成与 `MCP_KEYS` 的 key 完全一致 |
| 启动日志 `WARNING: 设置了 MCP_KEY_SLUGS 但没有 MCP_KEYS` | 只配了后缀，没配多 key | 补 `MCP_KEYS`，或删掉 `MCP_KEY_SLUGS` |
| 启动失败 `文件名后缀都是「x」，请改成不同的值` | 两个 key 给了同一后缀 | 改成各自不同的值；失败发生在建分组之前，不会残留数据 |
| `curl /chrome-mcp-1` 得 401 | 没带凭据 | 加 `-H 'Authorization: Bearer key1'` 或 `?token=key1` |
| `curl /chrome-mcp-2` 带 key1 得 403 | 用了别人的后缀 | 换成自己的后缀；后缀不构成越权，只会被拒 |
| `curl /chrome-mcp-1` 带 key1 得 403 | 后缀拼错（该 key 的实际后缀是别的） | 看启动日志 `下载名 chrome-mcp-<slug>`，或直接请求 `/chrome-mcp` 看 `Content-Disposition` |
| `curl /chrome-mcp-1` 得 404 | 单 `MCP_TOKEN` 模式，没有按 key 区分的文件 | 只用 `/chrome-mcp` |
| 下载后 `./chrome-mcp --version` 报 `Permission denied` | 没 `chmod +x` | `chmod +x chrome-mcp-1` |
| 模块报 `Timed out ... waiting for the WS endpoint URL` | `CDP_HTTP` 是 `https`（拿到了 `wss://`） | 改用明文端口，或设 `MCP_SHIM_CDP_URL=http://...` |

**六、这几种“两处都填”的写法**

- **同一个 key 在 `MCP_KEYS` 与 `MCP_KEY_SLUGS` 各出现一次**——这是正常且必需的，前者是凭据，
  后者是文件名。不是同一件事填两遍。
- **`MCP_KEY_SLUGS` 的值恰好等于 token**（`{"key1":"key1"}`）——合法，等价于不设，
  文件名 `chrome-mcp-key1`。
- **两个 key 用同一个后缀**——拒绝启动，因为一个 URL 只能对应一个文件。
- **`MCP_KEY_SLUGS` 写了 `MCP_KEYS` 里没有的 key**——警告并忽略；不会创建凭据，也不会变成
  可用文件名。
- **同一个配置名被两个 key 绑定**（`{"key1":"窗口1","key2":"窗口1"}`）——拒绝启动，
  和上面那条规则一样属于「歧义」类错误。

**七、只想要一个文件、不想要多 key**

不设 `MCP_KEYS`，设 `MCP_TOKEN`。此时只有 `/chrome-mcp` 一个下载入口：带对凭据得到预填
`MCP_TOKEN` 的版本，不带凭据得到通用脚本。`MCP_KEY_SLUGS` 在这种模式下不生效（会给警告）。

**多 key 按配置隔离，不按标签页隔离。** 一个 key 对应一个配置（也就是一个浏览器实例），
实例内部只绑定第一个页面；同一个实例里开多个标签页要靠 CDP 侧自己 `newPage()`，与 key 无关。

#### Chromium 沙箱

`root/usr/bin/chrome` 会根据 `CHROME_SANDBOX` 决定是否给 Chromium 加 `--no-sandbox`：

| 取值 | 行为 |
| --- | --- |
| `auto`（默认） | 沿用原逻辑：容器 PID 1 的 `Seccomp` 为 0 时不加，否则加 `--no-sandbox` |
| `1` / `true` / `yes` / `on` | 强制启用沙箱，不加 `--no-sandbox` |
| `0` / `false` / `no` / `off` | 强制关闭沙箱，加 `--no-sandbox` |
| 其它值 | 按 `auto` 处理 |

Docker 默认给容器套 seccomp 配置，`auto` 因此总是落到关闭沙箱这一支，Chromium 会在窗口顶部
显示「您使用的是不受支持的命令行标记：--no-sandbox」。容器里要真正启用沙箱，需要同时放开
seccomp 并设 `CHROME_SANDBOX=1`，此时不加 `--no-sandbox`，那条提示也不会出现：

```bash
docker run -d --name chromium-manager \
  --security-opt seccomp=unconfined \
  -e CHROME_SANDBOX=1 \
  ghcr.io/ldxw/chromium-manager:156.0.8078.79-1-mcp-keys
```

注意 `seccomp=unconfined` 会让整个容器失去 seccomp 过滤，不只是 Chromium。

这条提示只是 `--no-sandbox` 的警告文案，可以用 `--test-type` 把它藏掉，但沙箱仍然是关着的，
镜像不做这种遮盖。

镜像里的默认值是 `MCP_ENABLE_DESTRUCTIVE=0`、`MCP_TOKEN=` / `MCP_KEYS=` / `MCP_KEYS_FILE=` /
`MCP_KEY_SLUGS=` / `MCP_SHIM_CDP_URL=`（空）、`MCP_GROUP_NAME=mcp`、
`MCP_HTTP_ADDR=0.0.0.0:10102`、`CHROME_SANDBOX=auto`，
`docker run -e` 或 compose 的 `environment` 会覆盖它们。这几个变量都在
进程或浏览器启动时读取，MCP 相关的改了要重启容器才生效。

### 工具列表

默认 15 个，设置 `MCP_ENABLE_DESTRUCTIVE=1` 后 19 个：

- 只读：`list_profiles`、`get_profile`、`list_running`、`list_groups`、`list_proxies`
- 管理：`launch_profile`、`stop_profile`、`add_profile`、`update_profile`
- 浏览器：`navigate`、`read_page`、`click`、`fill`、`screenshot`、`eval_js`
- 破坏性（需开启）：`delete_profile`、`delete_proxy`、`delete_group`、`stop_all_profiles`

浏览器类工具要求目标配置已 `launch_profile`，内部通过 CDP 连接该配置的调试端口，
`launch` 后约 1–3 秒端口就绪。`screenshot` 未指定 `path` 时写入 `<DATA_DIR>/screenshots/`，
返回图片绝对路径，客户端可直接读取。

---

## 📄 许可证

MIT License

本项目来源、本仓库的修改，以及 Go / 前端依赖与 Docker 基础镜像各自的
许可义务，见 [NOTICE](NOTICE)。
