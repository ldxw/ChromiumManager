#!/usr/bin/env python3
"""校验已推送的多架构镜像在目标仓库里真的可用。

查的是仓库侧的实际状态，而不是 buildx 的退出码：buildx 成功只代表 manifest 被收下，
registry 并不会校验 manifest 里引用的 blob 是否真的存在。用法：

    verify-image.py <镜像引用> [--expect docker|any]

检查项：
  1. tag 指向的是多架构索引，而不是单个平台的 manifest；
  2. 平台集合恰好是 linux/amd64 与 linux/arm64（attestation 条目不计入）；
  3. 每个子 manifest 都能取到；
  4. 每个子 manifest 的 config 与全部 layer blob 都能取到（HEAD 200）。

--expect docker 要求索引是 Docker manifest list（ACR 的安全格式），any 则接受 OCI index。
"""

import argparse
import base64
import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request

DOCKER_LIST = "application/vnd.docker.distribution.manifest.list.v2+json"
DOCKER_MANIFEST = "application/vnd.docker.distribution.manifest.v2+json"
OCI_INDEX = "application/vnd.oci.image.index.v1+json"
OCI_MANIFEST = "application/vnd.oci.image.manifest.v1+json"
INDEX_TYPES = (DOCKER_LIST, OCI_INDEX)
MANIFEST_TYPES = (DOCKER_MANIFEST, OCI_MANIFEST)
ACCEPT = ", ".join((DOCKER_LIST, OCI_INDEX, DOCKER_MANIFEST, OCI_MANIFEST))
WANT_PLATFORMS = {"linux/amd64", "linux/arm64"}
DOCKER_HUB_AUTH_KEY = "https://index.docker.io/v1/"


def fail(message):
    sys.exit("校验失败：" + message)


def split_reference(ref):
    """拆成 (host, repository, tag)。没写 host 时按 Docker Hub 处理。"""
    if ":" not in ref.rsplit("/", 1)[-1]:
        fail("镜像引用必须带 tag：%s" % ref)
    head, tag = ref.rsplit(":", 1)
    if "/" in head:
        first, rest = head.split("/", 1)
        if "." in first or ":" in first or first == "localhost":
            return first, rest, tag
    return "docker.io", head, tag


def credentials(host):
    """从 docker login 留下的配置里取该仓库的用户名密码。"""
    try:
        with open(os.path.expanduser("~/.docker/config.json")) as f:
            config = json.load(f)
    except (OSError, ValueError):
        return None

    # Docker Hub 在 docker 配置里的键是 index.docker.io 这个 URL，不是 docker.io
    key = DOCKER_HUB_AUTH_KEY if host == "docker.io" else host
    auths = config.get("auths") or {}
    keys = [key, host, "https://" + host]
    for candidate in keys:
        entry = auths.get(candidate) or {}
        if entry.get("auth"):
            user, _, password = base64.b64decode(entry["auth"]).decode().partition(":")
            return user, password

    helper = (config.get("credHelpers") or {}).get(key) or config.get("credsStore")
    if helper:
        try:
            out = subprocess.run(
                ["docker-credential-" + helper, "get"],
                input=key, capture_output=True, text=True, check=True,
            ).stdout
            data = json.loads(out)
            return data.get("Username"), data.get("Secret")
        except (OSError, subprocess.CalledProcessError, ValueError):
            return None
    return None


def lower_keys(headers):
    """HTTP/2 返回的头名是小写，这里统一成小写键，取用时不依赖具体协议版本。"""
    return {key.lower(): value for key, value in headers.items()}


def http(url, token=None, method="GET", accept=None):
    """accept 默认不设：只有 manifest 请求需要那组 media type。

    阿里云的令牌端点（dockerauth.<region>.aliyuncs.com/auth）只接受 JSON，
    带上 manifest 的 Accept 会直接回 406，导致校验在申请令牌这一步就失败。
    """
    request = urllib.request.Request(url, method=method)
    if accept:
        request.add_header("Accept", accept)
    if token:
        request.add_header("Authorization", token)
    try:
        with urllib.request.urlopen(request, timeout=60) as response:
            return response.getcode(), response.read(), lower_keys(response.headers)
    except urllib.error.HTTPError as error:
        return error.code, error.read(), lower_keys(error.headers)


def token_from_challenge(challenge, host):
    params = dict(re.findall(r'(\w+)="([^"]*)"', challenge))
    realm = params.get("realm")
    if not realm:
        fail("无法解析仓库的认证挑战：%s" % challenge)
    query = []
    for key in ("service", "scope"):
        if params.get(key):
            query.append(key + "=" + urllib.parse.quote(params[key], safe=""))
    url = realm + ("?" + "&".join(query) if query else "")
    # 公共仓库允许匿名申请 pull 令牌；有登录凭据时带上，私有仓库才不会 401
    login = credentials(host)
    basic = None
    if login:
        basic = "Basic " + base64.b64encode(("%s:%s" % login).encode()).decode()
    code, body, _ = http(url, token=basic, accept="application/json")
    if code != 200:
        fail("向 %s 申请仓库令牌失败：HTTP %s" % (host, code))
    return "Bearer " + json.loads(body)["token"]


def fetch_manifest(base, reference, host, token):
    code, body, headers = http("%s/manifests/%s" % (base, reference), token=token, accept=ACCEPT)
    if code == 401 and token is None:
        challenge = headers.get("www-authenticate") or ""
        if challenge.lower().startswith("bearer"):
            token = token_from_challenge(challenge, host)
        else:
            login = credentials(host)
            if not login:
                fail("仓库要求认证，但找不到 %s 的 docker 登录信息" % host)
            token = "Basic " + base64.b64encode(("%s:%s" % login).encode()).decode()
        code, body, headers = http("%s/manifests/%s" % (base, reference), token=token, accept=ACCEPT)
    if code != 200:
        fail("读取 %s 失败：HTTP %s %s" % (reference, code, body[:200].decode(errors="replace")))
    return json.loads(body), token


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("image")
    parser.add_argument("--expect", choices=("docker", "any"), default="any")
    args = parser.parse_args()

    host, repository, tag = split_reference(args.image)
    # Docker Hub 的 API 端点是 registry-1.docker.io，docker.io 只解析到网页
    registry = "registry-1.docker.io" if host == "docker.io" else host
    scheme = "http" if registry.split(":")[0] in ("127.0.0.1", "localhost") else "https"
    base = "%s://%s/v2/%s" % (scheme, registry, repository)

    index, token = fetch_manifest(base, tag, host, None)
    media_type = index.get("mediaType") or ""
    if media_type not in INDEX_TYPES:
        fail("tag 指向的不是多架构索引，而是 %r，单平台镜像无法在多架构客户端正常拉取" % media_type)
    if args.expect == "docker" and media_type != DOCKER_LIST:
        fail("期望 Docker manifest list，实际 %s" % media_type)

    # attestation 条目是 architecture=unknown 的附加 manifest，不是可拉取的平台
    platforms = set()
    children = []
    for entry in index.get("manifests") or []:
        platform = entry.get("platform") or {}
        label = "%s/%s" % (platform.get("os") or "", platform.get("architecture") or "")
        if label == "unknown/unknown":
            continue
        platforms.add(label)
        children.append(entry)
    if platforms != WANT_PLATFORMS:
        fail("平台集合不符：实际 %s，期望 %s" % (sorted(platforms), sorted(WANT_PLATFORMS)))
    if len(children) != len(WANT_PLATFORMS):
        fail("索引里同一平台的 manifest 重复：%d 条" % len(children))

    checked_blobs = 0
    for entry in children:
        digest = entry["digest"]
        child, token = fetch_manifest(base, digest, host, token)
        if child.get("mediaType") not in MANIFEST_TYPES:
            fail("子 manifest %s 的类型异常：%r" % (digest[:19], child.get("mediaType")))
        blobs = [child["config"]] + list(child.get("layers") or [])
        if len(blobs) < 2:
            fail("子 manifest %s 只有 %d 个 blob，镜像内容不完整" % (digest[:19], len(blobs)))
        for blob in blobs:
            code, _, _ = http("%s/blobs/%s" % (base, blob["digest"]), token=token, method="HEAD")
            if code != 200:
                fail("子 manifest %s 引用的 blob %s 在仓库里不存在（HTTP %s），"
                     "说明这次推送只写入了 manifest，实际内容为空"
                     % (digest[:19], blob["digest"][:19], code))
            checked_blobs += 1

    print("校验通过：%s %s，平台 %s，%d 个子 manifest、%d 个 blob 全部可达"
          % (args.image, media_type, ", ".join(sorted(platforms)), len(children), checked_blobs))


main()
