#!/usr/bin/env bash
# =============================================================================
# Sub2API（sub2api-so 二开版）Docker 镜像离线打包脚本
# =============================================================================
# 复用官方根 Dockerfile 的分支副本 deploy/Dockerfile.bundle（唯一差异：容器 pnpm
# 固定 11.9.0 与宿主机 lock 维护版本一致，原因见该文件头注释），在此之上补齐离线分发能力：
#   1. 版本号自动推导：UPSTREAM-BASE 哈希 -> sub2api-original 中对应的上游 tag
#      （如 v0.2.13 -> 0.2.13；可用 VERSION=x.y.z 覆盖）
#   2. 注入 VERSION / COMMIT / DATE 构建参数（写入二进制版本信息）
#   3. docker save 导出镜像 tar（GZIP=1 输出 .tar.gz），附 sha256 校验文件
#   4. 组装离线部署包：镜像 tar + docker-compose.yml + .env.example + README.md
#
# 用法（Git Bash / Linux，任意目录执行）：
#   bash deploy/build-docker-tar.sh                      # 全默认
#   VERSION=0.2.13 bash deploy/build-docker-tar.sh       # 手动指定版本号
#   GZIP=1 bash deploy/build-docker-tar.sh               # 压缩为 .tar.gz
#   OUT_DIR=/tmp/sub2api-bundle bash deploy/build-docker-tar.sh
#   NPM_REGISTRY=https://registry.npmmirror.com bash deploy/build-docker-tar.sh
#
# 产物：dist/docker-bundle/ —— 整个目录 scp 到服务器即可部署（见包内 README.md）。
# 网络口径：Go 模块走 goproxy.cn + sum.golang.google.cn（与官方 Dockerfile 默认一致）。
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

IMAGE_NAME="${IMAGE_NAME:-sub2api}"
OUT_DIR="${OUT_DIR:-${REPO_ROOT}/dist/docker-bundle}"
UPSTREAM_DIR="${UPSTREAM_DIR:-$(cd "${REPO_ROOT}/.." && pwd)/sub2api-original}"
GOPROXY_ARG="${GOPROXY:-https://goproxy.cn,direct}"
GOSUMDB_ARG="${GOSUMDB:-sum.golang.google.cn}"
GZIP="${GZIP:-0}"

# ---------------------------------------------------------------------------
# 1. 版本号推导：UPSTREAM-BASE -> 上游 tag
# ---------------------------------------------------------------------------
BASE_HASH="$(tr -d '\r\n' < "${REPO_ROOT}/UPSTREAM-BASE")"
[ -n "${BASE_HASH}" ] || { echo "[错误] UPSTREAM-BASE 文件为空" >&2; exit 1; }

if [ -z "${VERSION:-}" ]; then
  # 精确 tag 得 vX.Y.Z；tag 后的提交得 vX.Y.Z-N-gHASH，两者都取 tag 前缀
  DESCRIBE="$(git -C "${UPSTREAM_DIR}" describe --tags --match 'v[0-9]*' "${BASE_HASH}" 2>/dev/null || true)"
  case "${DESCRIBE}" in
    v[0-9]*) VERSION="${DESCRIBE%%-*}"; VERSION="${VERSION#v}" ;;
    *)       VERSION="$(tr -d '\r\n' < "${REPO_ROOT}/backend/cmd/server/VERSION")" ;;
  esac
fi
[ -n "${VERSION}" ] || { echo "[错误] 无法推导版本号，请以 VERSION=x.y.z 指定" >&2; exit 1; }

COMMIT="$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo docker)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

FULL_IMAGE="${IMAGE_NAME}:${VERSION}"
echo "==> 版本        : ${VERSION}"
echo "==> 上游基准    : ${BASE_HASH}"
echo "==> 二开 commit : ${COMMIT}"
echo "==> 镜像        : ${FULL_IMAGE}"

# ---------------------------------------------------------------------------
# 2. 构建镜像（deploy/Dockerfile.bundle：根 Dockerfile 分支，pnpm@11.9.0）
# ---------------------------------------------------------------------------
BUILD_ARGS=(
  --build-arg "VERSION=${VERSION}"
  --build-arg "COMMIT=${COMMIT}"
  --build-arg "DATE=${BUILD_DATE}"
  --build-arg "GOPROXY=${GOPROXY_ARG}"
  --build-arg "GOSUMDB=${GOSUMDB_ARG}"
)
if [ -n "${NPM_REGISTRY:-}" ]; then
  BUILD_ARGS+=(--build-arg "NPM_CONFIG_REGISTRY=${NPM_REGISTRY}")
fi

docker build \
  -t "${FULL_IMAGE}" \
  "${BUILD_ARGS[@]}" \
  -f "${SCRIPT_DIR}/Dockerfile.bundle" \
  "${REPO_ROOT}"

# latest 仅作本地便捷标签；部署包内 .env 始终固定版本号 tag
docker tag "${FULL_IMAGE}" "${IMAGE_NAME}:latest"

IMG_ID="$(docker image inspect "${FULL_IMAGE}" --format '{{.Id}}')"
IMG_SIZE="$(docker image inspect "${FULL_IMAGE}" --format '{{.Size}}')"
echo "==> 构建完成: id=${IMG_ID}  size=$((IMG_SIZE / 1024 / 1024))MB"

# ---------------------------------------------------------------------------
# 3. 导出 tar（可选 gzip）+ sha256 校验
# ---------------------------------------------------------------------------
mkdir -p "${OUT_DIR}"
TAR_FILE="${OUT_DIR}/${IMAGE_NAME}-${VERSION}-docker-image.tar"
if [ "${GZIP}" = "1" ]; then
  TAR_FILE="${TAR_FILE}.gz"
  docker save "${FULL_IMAGE}" | gzip > "${TAR_FILE}"
else
  docker save -o "${TAR_FILE}" "${FULL_IMAGE}"
fi
( cd "$(dirname "${TAR_FILE}")" && sha256sum "$(basename "${TAR_FILE}")" > "${TAR_FILE}.sha256" )

echo "==> 镜像已导出: ${TAR_FILE} ($(du -h "${TAR_FILE}" | cut -f1))"

# ---------------------------------------------------------------------------
# 4. 组装离线部署包
# ---------------------------------------------------------------------------
cp "${SCRIPT_DIR}/docker-compose.bundle.yml" "${OUT_DIR}/docker-compose.yml"

cat > "${OUT_DIR}/.env.example" <<EOF
# =============================================================================
# Sub2API ${VERSION} 离线部署配置（复制为 .env 后按需修改）
# =============================================================================

# 镜像 tag：docker load 导入后即存在；升级新版本 tar 后同步改这里
SUB2API_IMAGE=${IMAGE_NAME}:${VERSION}

# 对外端口（仅本机/内网访问可改 BIND_HOST=127.0.0.1）
BIND_HOST=0.0.0.0
SERVER_PORT=8080

# —— 必填 ——
POSTGRES_PASSWORD=
ADMIN_EMAIL=admin@sub2api.local
ADMIN_PASSWORD=

# —— 可选 ——
#TZ=Asia/Shanghai
#JWT_SECRET=
#REDIS_PASSWORD=
#SERVER_MODE=release
EOF

cat > "${OUT_DIR}/README.md" <<EOF
# Sub2API ${VERSION} 离线部署包

- 镜像 tar：$(basename "${TAR_FILE}")（校验：sha256sum -c "$(basename "${TAR_FILE}").sha256"）
- 应用版本：${VERSION}（基于上游 v${VERSION}，二开 commit ${COMMIT}）
- 构建时间：${BUILD_DATE} (UTC)
- 包内文件：镜像 tar + docker-compose.yml（应用 + PostgreSQL 18 + Redis 8）+ .env.example

## 部署步骤（全新部署）

1. 将整个目录传输到服务器（scp/rsync 均可）
2. 导入应用镜像：
       docker load -i $(basename "${TAR_FILE}")
   服务器还需 postgres:18-alpine 与 redis:8-alpine 两个基础镜像：
   可联网时 docker compose 会自动拉取；离线服务器请提前 load 这两个镜像的 tar
3. 配置环境变量：
       cp .env.example .env && vi .env
   必填 POSTGRES_PASSWORD 与 ADMIN_PASSWORD（可用 openssl rand -hex 16 生成密码）
4. 启动：
       docker compose up -d
5. 验证：
       docker compose ps                          # 三个服务均应为 Up (healthy)
       curl http://127.0.0.1:8080/health          # 正常返回 ok
6. 浏览器访问 http://<服务器IP>:8080，用 .env 中的 ADMIN_EMAIL / ADMIN_PASSWORD 登录

## 升级已有实例（重要）

- 升级前务必备份数据库：
      docker exec sub2api-postgres pg_dump -U sub2api -d sub2api > sub2api_backup_\$(date +%F).sql
- 本包的 PG/Redis 使用独立命名卷（sub2api_pgdata / sub2api_redisdata），启动不会触碰
  已有实例的数据卷；若要让新版本接管旧数据，不要直接 up，先在 .env 中把连接指向
  现有数据库/Redis，或按官方备份恢复流程迁移数据
- 应用启动时自动执行数据库迁移，跨多个版本升级迁移不可逆，请先备份再操作

## 常用运维命令

      docker compose logs -f sub2api     # 应用日志
      docker compose restart sub2api     # 重启应用
      docker compose down                # 停止（不加 -v 不删数据卷）
EOF

echo "==> 离线部署包已生成: ${OUT_DIR}"
ls -lh "${OUT_DIR}"
