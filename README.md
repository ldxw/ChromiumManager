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
    image: ghcr.io/ldxw/chromium-manager:156.0.8078.79-1-mcp
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
  -p 3001:3001 \
  -p 10102:10102 \
  -v ./config:/config \
  --restart unless-stopped \
  ghcr.io/ldxw/chromium-manager:156.0.8078.79-1-mcp
```

镜像每个版本只有一个标签，形式为 `<Chromium 版本号>-mcp`，上例中的
`156.0.8078.79-1` 即二进制来源的 Release 版本。不提供 `latest`。

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

`PUID` / `PGID` / `TZ` / `LC_ALL` / `CUSTOM_USER` / `PASSWORD` / `TITLE` / `NO_DECOR` 由继承的
linuxserver 基础镜像处理。MCP 相关变量（`MCP_TOKEN`、`MCP_KEYS` 等）见
「🤖 MCP 支持 → MCP 环境变量」。

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

`/mcp` 端点在 nginx 里单独 `auth_basic off;`，不使用这两个变量，只用 `MCP_TOKEN`，
详见「🤖 MCP 支持」。

如果容器暴露在公网，建议一定要设 `PASSWORD`（用户名可保持默认 `abc`），否则任何能连到
3001 的人都可以直接操作管理界面。

镜像默认发布到 GHCR（`ghcr.io/<owner>/chromium-manager`），也可以推阿里云镜像仓库或
Docker Hub，构建由 `.github/workflows/docker-image.yml` 完成：

- 手动触发，可选填 `release_tag`（二进制来源）与 `image_tag`（镜像标签）；
  两者留空时，前者取仓库最新的正式 Release，后者跟随前者。
- `push_ghcr` **默认勾选**，取消后本次完全不碰 GHCR，可以只推阿里云或 Docker Hub。
  GHCR 鉴权用仓库自带的 `GITHUB_TOKEN`，不需要额外 secret。
- 勾选 `push_aliyun` 后推送一份到
  `<ALIYUN_REGISTRY>/<ALIYUN_NAMESPACE>/chromium-manager:<版本号>-mcp`，
  需要在仓库 Secrets 里配置 `ALIYUN_REGISTRY`、`ALIYUN_NAMESPACE`、
  `ALIYUN_USERNAME`、`ALIYUN_PASSWORD`；缺任意一个会在构建前直接失败。
- 勾选 `push_dockerhub` 后推送一份到
  `<DOCKERHUB_USERNAME>/chromium-manager:<版本号>-mcp`，需要在仓库 Secrets 里配置
  `DOCKERHUB_USERNAME` 与 `DOCKERHUB_TOKEN`；缺任意一个会在构建前直接失败。
- 三个开关可以任意组合，至少勾选一个；全不勾选会在解析参数阶段直接失败。
- 构建 `linux/amd64` 与 `linux/arm64` 两个架构，标签固定带 `-mcp` 后缀。
- 构建失败会自动重试最多 3 次，用于绕过拉取基础镜像时的偶发 429。
- GHCR 与 Docker Hub 保留 buildx 默认的 provenance attestation。勾选 `push_aliyun` 时，
  阿里云那份会**单独再推一次**并关闭 provenance 与 SBOM，因为阿里云 ACR 不认
  `application/vnd.oci.empty.v1+json` 这个 attestation manifest 类型，会拒绝整次推送。
  两次推送复用同一个 buildkit 层缓存，第二次只重新组 manifest 并上传，不重复编译。
  阿里云那份的 manifest list 因此只有 amd64 / arm64 两条真实镜像，无 attestation。

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

因为端点现在可以从容器外触达，**强烈建议设置 `MCP_TOKEN`**；不设置时任何能连到该端口的人
都能调用全部已启用的工具。

### 外部直连端口

除 nginx 反代之外，程序还会另起一个独立的 MCP 监听，默认 `0.0.0.0:10102`，只挂 `/mcp`
和 `/healthz` 两个路由，不暴露管理界面，也不经过 Basic Auth：

```bash
docker run ... -p 10102:10102 ...

curl -X POST http://<宿主机>:10102/mcp -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <你的 MCP_TOKEN>' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

curl http://<宿主机>:10102/healthz   # 返回 ok，可用于探活
```

地址由 `MCP_HTTP_ADDR` 控制：改成 `127.0.0.1:10102` 可退回仅本机，设为 `off` 完全关闭。
鉴权同样读 `MCP_TOKEN`，为空时不校验。

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

### 环境变量

| 变量 | 作用 |
| --- | --- |
| `DATA_DIR` | 数据目录，默认为 `./data` |
| `MCP_ENABLE_DESTRUCTIVE` | 设为 `1` 才注册删除类工具（`delete_profile` / `delete_proxy` / `delete_group` / `stop_all_profiles`） |
| `MCP_TOKEN` | 仅 HTTP 传输生效；设置后要求 `Authorization: Bearer <token>`，未设置不校验。反代与外部端口都在容器外可触达，建议始终设置 |
| `MCP_HTTP_ADDR` | 外部直连监听地址，默认 `0.0.0.0:10102`；设为 `127.0.0.1:10102` 仅本机可用，设为 `off` 关闭 |
| `CHROME_SANDBOX` | 控制 Chromium 沙箱，见下方说明，默认 `auto` |

需要 HTTP 传输鉴权时，在 `docker run` 里再加 `-e MCP_TOKEN=<随机串>`（compose 中放开被注释的那行），
客户端请求必须带 `Authorization: Bearer <随机串>`，否则返回 401。

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
  ghcr.io/ldxw/chromium-manager:156.0.8078.79-1-mcp
```

注意 `seccomp=unconfined` 会让整个容器失去 seccomp 过滤，不只是 Chromium。

这条提示只是 `--no-sandbox` 的警告文案，可以用 `--test-type` 把它藏掉，但沙箱仍然是关着的，
镜像不做这种遮盖。

镜像里的默认值是 `MCP_ENABLE_DESTRUCTIVE=0`、`MCP_TOKEN=`（空）、`MCP_HTTP_ADDR=0.0.0.0:10102`、
`CHROME_SANDBOX=auto`，`docker run -e` 或 compose 的 `environment` 会覆盖它们。这几个变量都在
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
