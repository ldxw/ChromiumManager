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

### Docker Compose

```yaml
services:
  chromium-manager:
    image: ghcr.io/ldxw/chromium-manager:156.0.8078.79-1-mcp
    container_name: chromium-manager
    shm_size: 1gb
    environment:
      - PUID=1000
      - PGID=1000
      - TZ=Asia/Shanghai
      - LC_ALL=zh_CN.UTF-8
      # MCP 相关，按需开启，详见「🤖 MCP 支持」
      - MCP_ENABLE_DESTRUCTIVE=0
      # - MCP_TOKEN=换成你自己的随机串
      # - MCP_HTTP_ADDR=0.0.0.0:10102
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

### Docker CLI

```bash
docker run -d \
  --name chromium-manager \
  --shm-size=1gb \
  -e PUID=1000 \
  -e PGID=1000 \
  -e TZ=Asia/Shanghai \
  -e LC_ALL=zh_CN.UTF-8 \
  -e MCP_ENABLE_DESTRUCTIVE=0 \
  -p 3001:3001 \
  -p 10102:10102 \
  -v ./config:/config \
  --restart unless-stopped \
  ghcr.io/ldxw/chromium-manager:156.0.8078.79-1-mcp
```

镜像每个版本只有一个标签，形式为 `<Chromium 版本号>-mcp`，上例中的
`156.0.8078.79-1` 即二进制来源的 Release 版本。不提供 `latest`。

镜像默认发布到 GHCR（`ghcr.io/<owner>/chromium-manager`），可选同时推一份到阿里云
镜像仓库或 Docker Hub，构建由 `.github/workflows/docker-image.yml` 完成：

- 手动触发，可选填 `release_tag`（二进制来源）与 `image_tag`（镜像标签）；
  两者留空时，前者取仓库最新的正式 Release，后者跟随前者。
- GHCR 鉴权用仓库自带的 `GITHUB_TOKEN`，不需要额外 secret。
- 勾选 `push_aliyun` 后额外推送一份到
  `<ALIYUN_REGISTRY>/<ALIYUN_NAMESPACE>/chromium-manager:<版本号>-mcp`，
  需要在仓库 Secrets 里配置 `ALIYUN_REGISTRY`、`ALIYUN_NAMESPACE`、
  `ALIYUN_USERNAME`、`ALIYUN_PASSWORD`；缺任意一个会在构建前直接失败。
- 勾选 `push_dockerhub` 后额外推送一份到
  `<DOCKERHUB_USERNAME>/chromium-manager:<版本号>-mcp`，需要在仓库 Secrets 里配置
  `DOCKERHUB_USERNAME` 与 `DOCKERHUB_TOKEN`；缺任意一个会在构建前直接失败。
- 两个开关可以同时勾选，三个仓库共用同一次多架构构建结果。
- 构建 `linux/amd64` 与 `linux/arm64` 两个架构，标签固定带 `-mcp` 后缀。
- 构建失败会自动重试最多 3 次，用于绕过拉取基础镜像时的偶发 429。

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
都会卡在某一层，因此 `/mcp` 只认 Bearer；Web 界面仍走 `CUSTOM_USER` / `PASSWORD`
的 Basic Auth，两者互不影响。

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

需要 HTTP 传输鉴权时，在 `docker run` 里再加 `-e MCP_TOKEN=<随机串>`（compose 中放开被注释的那行），
客户端请求必须带 `Authorization: Bearer <随机串>`，否则返回 401。

镜像里的默认值是 `MCP_ENABLE_DESTRUCTIVE=0`、`MCP_TOKEN=`（空）、`MCP_HTTP_ADDR=0.0.0.0:10102`，
`docker run -e` 或 compose 的 `environment` 会覆盖它们。三个变量都在进程启动时读取一次，
改了要重启容器才生效。

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
