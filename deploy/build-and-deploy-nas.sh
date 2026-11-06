#!/usr/bin/env bash
# =============================================================================
# Sub2API（sub2api-so 二开版）NAS 正式实例一键构建 + 推送部署脚本（开发机远端部署版）
# =============================================================================
# 2026-11 环境迁移后在开发机（Debian 13）执行，远端操作全部经 SSH 到 NAS：
#   1. 本机构建镜像 sub2api:<上游版本>-so-<日期>（根目录 Dockerfile，注入
#      VERSION/COMMIT/DATE；同日多次构建自动追加 -2/-3…，按 NAS 侧已有 tag
#      探测递增，旧 tag 永不覆盖）
#   2. docker save 导出 tar（+sha256）到本机 dist/（临时，推送校验通过后
#      删除，KEEP_LOCAL_TAR=1 保留）
#   3. scp 推送 tar 到 NAS /vol2/1000/DockerImages 归档，远端 sha256 校验
#      后 docker load（并核对 load 出的镜像名）
#   4. 部署前经 SSH 在 NAS 上 pg_dump 备份数据库到 <COMPOSE_DIR>/data/
#   5. 备份并改写 NAS <COMPOSE_DIR>/docker-compose.yml 的 image: tag
#   6. 远端 docker compose up -d 重建容器，验证 healthy + /health
#
# 用法（在开发机执行，任意目录）：
#   bash deploy/build-and-deploy-nas.sh              # 全流程（交互确认）
#   bash deploy/build-and-deploy-nas.sh -y           # 跳过确认（脚本化用）
#   TAR_ONLY=1 bash deploy/build-and-deploy-nas.sh   # 只构建 + 本机出 tar，不部署
#   VERSION=0.2.18 bash deploy/build-and-deploy-nas.sh
#   COMMIT=11e53b1+oc-reasoning-fix bash ...         # 手动指定 commit 展示值
#   GZIP=1 bash ...                                  # tar 压缩为 .tar.gz（减小传输量，load 自动解压）
#
# 一次性前置：
#   1) 本机 docker：当前用户在 docker 组；
#   2) SSH 免密：ssh-copy-id <SSH_HOST>（脚本用 BatchMode 非交互，无免密
#      会在预检失败并给指引）。
#
# 环境变量（均可缺省）：
#   SSH_HOST（NAS 的 ssh 别名，默认 fnos）/ VERSION / COMMIT / TAG（显式指定
#   镜像 tag，跳过同日 -N 递增）/ GZIP / KEEP_LOCAL_TAR /
#   TAR_DIR / COMPOSE_DIR（均为 NAS 侧路径）/ HEALTH_URL /
#   SKIP_DB_BACKUP=1（跳过部署前备份，不推荐）/ NPM_REGISTRY / GOPROXY / GOSUMDB
#
# 回滚：把 NAS 上 compose 的 image: 改回旧 tag 再 docker compose up -d（旧镜像与
# compose 备份均保留在 NAS，见脚本结尾打印的指引）。跨版本升级会触发自动数据库
# 迁移，部署前备份默认开启，请勿随意跳过。
#
# 注意：容器重建会触发 fnOS SMB 共享丢失 bug（docker/code/DockerImages 从
# Windows 侧短暂消失），守护脚本 /vol2/1000/docker/fix-smb-shares.sh 数分钟内
# 自动恢复，无需人工干预。
#
# 与 deploy/build-docker-tar.sh 的分工：那个脚本产出「离线分发包」（镜像 tar +
# bundle compose + .env + README），用于全新服务器离线安装；本脚本面向 NAS 上的
# Compose 正式实例（外部 PG/Redis）的版本更新。
#
# 构建路径用【根目录 Dockerfile】（pnpm@9，与 frontend/pnpm-lock.yaml 的 pnpm 9
# 格式匹配，见 2026-10-21 提交 2a53a42）。注意 deploy/Dockerfile.bundle
# （pnpm@11.9.0）自该次 lockfile 重生成后与 frozen 校验不兼容（pnpm 10+ 视
# package.json#pnpm.overrides 为死配置，只认 pnpm-workspace.yaml#overrides），
# 且缺 resources 权限修复，勿用于构建；如需换回可 DOCKERFILE= 覆盖。
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

IMAGE_NAME="${IMAGE_NAME:-sub2api}"
DOCKERFILE="${DOCKERFILE:-${REPO_ROOT}/Dockerfile}"
SSH_HOST="${SSH_HOST:-fnos}"                             # NAS SSH 目标（~/.ssh/config 别名，须免密）
TAR_DIR="${TAR_DIR:-/vol2/1000/DockerImages}"            # NAS 侧镜像 tar 归档目录
COMPOSE_DIR="${COMPOSE_DIR:-/vol2/1000/docker/sub2api}"  # NAS 侧 compose 目录
COMPOSE_FILE="${COMPOSE_DIR}/docker-compose.yml"
LOCAL_TAR_DIR="${LOCAL_TAR_DIR:-${REPO_ROOT}/dist}"      # 本机临时 tar 目录（dist 已 gitignore）
HEALTH_URL="${HEALTH_URL:-http://192.168.1.71:8033/health}"
GOPROXY_ARG="${GOPROXY:-https://goproxy.cn,direct}"
GOSUMDB_ARG="${GOSUMDB:-sum.golang.google.cn}"
GZIP="${GZIP:-0}"
KEEP_LOCAL_TAR="${KEEP_LOCAL_TAR:-0}"
SKIP_DB_BACKUP="${SKIP_DB_BACKUP:-0}"
TAR_ONLY="${TAR_ONLY:-0}"
YES="${YES:-0}"

# SSH 统一封装：BatchMode 禁交互（无免密立即失败）；accept-new 首连自动收录主机指纹。
# 远端多步命令一律走 `rsh bash -s -- <参数> <<'EOF'` 形态，避免 ssh 拼参数丢引号。
SSH_OPTS=(-o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=10)
rsh() { ssh "${SSH_OPTS[@]}" "${SSH_HOST}" "$@"; }

# ---------------------------------------------------------------------------
# 0. 参数解析
# ---------------------------------------------------------------------------
for arg in "$@"; do
  case "${arg}" in
    -y|--yes) YES=1 ;;
    -h|--help)
      sed -n '2,53p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "[错误] 未知参数: ${arg}（支持 -y/--yes/-h）" >&2
      exit 1
      ;;
  esac
done

# ---------------------------------------------------------------------------
# 1. 前置检查：本机 docker / SSH 免密与远端 docker+compose / compose 文件
# ---------------------------------------------------------------------------
command -v docker >/dev/null 2>&1 || { echo "[错误] 未找到 docker 命令" >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo "[错误] 本机 docker daemon 不可用（当前用户 $(id -un) 是否在 docker 组？）" >&2; exit 1; }
[ -f "${DOCKERFILE}" ] || { echo "[错误] 缺少 ${DOCKERFILE}" >&2; exit 1; }
if [ "${TAR_ONLY}" != "1" ]; then
  if ! rsh 'docker info >/dev/null 2>&1'; then
    echo "[错误] SSH 免密登录 ${SSH_HOST} 失败或远端 docker 不可用" >&2
    echo "       首次配置：ssh-copy-id ${SSH_HOST}" >&2
    exit 1
  fi
  rsh bash -s -- "${COMPOSE_FILE}" <<'EOF'
set -e
[ -f "$1" ] || { echo "[错误] 远端缺少 compose 文件: $1" >&2; exit 1; }
docker compose version >/dev/null 2>&1 || { echo "[错误] 远端 docker compose 子命令不可用" >&2; exit 1; }
EOF
fi

# ---------------------------------------------------------------------------
# 2. 版本号推导：UPSTREAM-BASE -> 上游 tag（与 build-docker-tar.sh 同逻辑）
# ---------------------------------------------------------------------------
BASE_HASH="$(tr -d '\r\n' < "${REPO_ROOT}/UPSTREAM-BASE")"
[ -n "${BASE_HASH}" ] || { echo "[错误] UPSTREAM-BASE 文件为空" >&2; exit 1; }

if [ -z "${VERSION:-}" ]; then
  UPSTREAM_DIR="${UPSTREAM_DIR:-$(cd "${REPO_ROOT}/.." && pwd)/sub2api-original}"
  DESCRIBE="$(git -C "${UPSTREAM_DIR}" describe --tags --match 'v[0-9]*' "${BASE_HASH}" 2>/dev/null || true)"
  case "${DESCRIBE}" in
    v[0-9]*) VERSION="${DESCRIBE%%-*}"; VERSION="${VERSION#v}" ;;
    *)       VERSION="$(tr -d '\r\n' < "${REPO_ROOT}/backend/cmd/server/VERSION")" ;;
  esac
fi
[ -n "${VERSION}" ] || { echo "[错误] 无法推导版本号，请以 VERSION=x.y.z 指定" >&2; exit 1; }

if [ -z "${COMMIT:-}" ]; then
  COMMIT="$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo docker)"
  if [ -n "$(git -C "${REPO_ROOT}" status --porcelain 2>/dev/null)" ]; then
    COMMIT="${COMMIT}-dirty"
  fi
fi
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# ---------------------------------------------------------------------------
# 3. 镜像 tag：<版本>-so-<日期>；同日重复构建自动追加 -2/-3…（旧 tag 不覆盖）。
#    递增探测查部署目标侧（NAS 远端镜像库；TAR_ONLY 时查本机）；
#    TAG= 显式指定时跳过探测（重跑补步骤时钉住同一 tag）。
# ---------------------------------------------------------------------------
TAG_DATE="$(TZ=Asia/Shanghai date +%Y%m%d)"
image_in_use() {  # $1=tag：探测该 tag 是否已被占用
  if [ "${TAR_ONLY}" = "1" ]; then
    docker image inspect "${IMAGE_NAME}:$1" >/dev/null 2>&1
  else
    rsh "docker image inspect '${IMAGE_NAME}:$1' >/dev/null 2>&1"
  fi
}
if [ -n "${TAG:-}" ]; then
  NEW_TAG="${TAG}"
else
  TAG_BASE="${VERSION}-so-${TAG_DATE}"
  NEW_TAG="${TAG_BASE}"
  SUFFIX=2
  while image_in_use "${NEW_TAG}"; do
    NEW_TAG="${TAG_BASE}-${SUFFIX}"
    SUFFIX=$((SUFFIX + 1))
  done
fi

FULL_IMAGE="${IMAGE_NAME}:${NEW_TAG}"
TAR_NAME="${IMAGE_NAME}-${NEW_TAG}-docker-image.tar"
[ "${GZIP}" = "1" ] && TAR_NAME="${TAR_NAME}.gz"
LOCAL_TAR_FILE="${LOCAL_TAR_DIR}/${TAR_NAME}"
REMOTE_TAR_FILE="${TAR_DIR}/${TAR_NAME}"

# 当前线上镜像（用于确认展示与回滚指引）
OLD_TAG=""
if [ "${TAR_ONLY}" != "1" ]; then
  OLD_TAG="$(rsh bash -s -- "${COMPOSE_FILE}" "${IMAGE_NAME}" <<'EOF'
sed -n -E "s|^[[:space:]]*image:[[:space:]]*$2:([^[:space:]]+).*|\1|p" "$1" | head -1
EOF
)"
fi

# ---------------------------------------------------------------------------
# 4. 打印计划并确认
# ---------------------------------------------------------------------------
echo "==> 版本        : ${VERSION}"
echo "==> 上游基准    : ${BASE_HASH}"
echo "==> 二开 commit : ${COMMIT}"
echo "==> 新镜像      : ${FULL_IMAGE}"
if [ "${TAR_ONLY}" = "1" ]; then
  echo "==> 模式        : 仅构建 + 本机出 tar（不部署）"
  echo "==> tar 输出    : ${LOCAL_TAR_FILE}"
else
  echo "==> NAS 目标    : ${SSH_HOST}"
  echo "==> tar 本机临时: ${LOCAL_TAR_FILE}（校验通过后$([ "${KEEP_LOCAL_TAR}" = "1" ] && echo '保留' || echo '删除，KEEP_LOCAL_TAR=1 可保留')）"
  echo "==> tar NAS 归档: ${SSH_HOST}:${REMOTE_TAR_FILE}"
  echo "==> 部署目标    : ${SSH_HOST}:${COMPOSE_FILE}"
  echo "==> 当前线上    : ${IMAGE_NAME}:${OLD_TAG:-（未读到，首次部署？）}"
  [ "${SKIP_DB_BACKUP}" = "1" ] && echo "==> 数据库备份  : 跳过（SKIP_DB_BACKUP=1）" || echo "==> 数据库备份  : 部署前 pg_dump 到 ${SSH_HOST}:${COMPOSE_DIR}/data/"
fi

if [ "${YES}" != "1" ]; then
  ANSWER=""
  read -r -p "确认执行？[y/N] " ANSWER || true
  case "${ANSWER}" in
    y|Y|yes|YES) ;;
    *) echo "已取消。"; exit 0 ;;
  esac
fi

# ---------------------------------------------------------------------------
# 5. 本机构建镜像（根目录 Dockerfile：pnpm@9 与 lockfile 格式匹配；见头注释）
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

echo "==> 开始构建（Dockerfile: ${DOCKERFILE}；多阶段，首次或缓存失效时耗时较长）…"
docker build \
  -t "${FULL_IMAGE}" \
  "${BUILD_ARGS[@]}" \
  -f "${DOCKERFILE}" \
  "${REPO_ROOT}"

IMG_ID="$(docker image inspect "${FULL_IMAGE}" --format '{{.Id}}')"
IMG_SIZE="$(docker image inspect "${FULL_IMAGE}" --format '{{.Size}}')"
echo "==> 构建完成: id=${IMG_ID}  size=$((IMG_SIZE / 1024 / 1024))MB"

# ---------------------------------------------------------------------------
# 6. 本机导出 tar（可选 gzip）+ sha256，落 ${LOCAL_TAR_DIR}
# ---------------------------------------------------------------------------
mkdir -p "${LOCAL_TAR_DIR}"
if [ "${GZIP}" = "1" ]; then
  docker save "${FULL_IMAGE}" | gzip > "${LOCAL_TAR_FILE}"
  gzip -t "${LOCAL_TAR_FILE}"   # 校验落盘完整（磁盘写满/中断在此暴露）
else
  docker save -o "${LOCAL_TAR_FILE}" "${FULL_IMAGE}"
fi
( cd "${LOCAL_TAR_DIR}" && sha256sum "${TAR_NAME}" > "${TAR_NAME}.sha256" )
echo "==> 本机 tar 导出: ${LOCAL_TAR_FILE} ($(du -h "${LOCAL_TAR_FILE}" | cut -f1))"

if [ "${TAR_ONLY}" = "1" ]; then
  echo "==> TAR_ONLY=1，流程结束（未部署）。"
  exit 0
fi

# ---------------------------------------------------------------------------
# 7. 推送 tar 到 NAS：scp + 远端 sha256 校验 + docker load（核对镜像名）
# ---------------------------------------------------------------------------
echo "==> 推送 tar 到 ${SSH_HOST}:${TAR_DIR}/ …"
rsh "mkdir -p '${TAR_DIR}'"
scp "${SSH_OPTS[@]}" "${LOCAL_TAR_FILE}" "${LOCAL_TAR_DIR}/${TAR_NAME}.sha256" "${SSH_HOST}:${TAR_DIR}/"
LOADED_IMAGE="$(rsh bash -s -- "${REMOTE_TAR_FILE}" <<'EOF'
set -e
cd "$(dirname "$1")"
sha256sum -c "$(basename "$1").sha256" >/dev/null
echo "    sha256 校验通过" >&2
out=$(docker load -i "$1")
echo "$out" >&2
echo "$out" | sed -n 's/^Loaded image: //p' | tail -n1
EOF
)"
if [ "${LOADED_IMAGE}" != "${FULL_IMAGE}" ]; then
  echo "[错误] 远端 load 出 ${LOADED_IMAGE:-（未解析出镜像名）}，与 ${FULL_IMAGE} 不一致" >&2
  exit 1
fi
if [ "${KEEP_LOCAL_TAR}" != "1" ]; then
  rm -f "${LOCAL_TAR_FILE}" "${LOCAL_TAR_DIR}/${TAR_NAME}.sha256"
fi
echo "==> NAS 镜像归档: ${SSH_HOST}:${REMOTE_TAR_FILE}"

# ---------------------------------------------------------------------------
# 8. 部署前数据库备份（NAS 上容器内 pg_dump；容器不存在则跳过）
# ---------------------------------------------------------------------------
BACKUP_FILE=""
if [ "${SKIP_DB_BACKUP}" != "1" ]; then
  BACKUP_FILE="${COMPOSE_DIR}/data/backup_predeploy_${TAG_DATE}-$(TZ=Asia/Shanghai date +%H%M%S).sql"
  echo "==> 备份数据库 -> ${SSH_HOST}:${BACKUP_FILE}"
  rsh bash -s -- "${COMPOSE_DIR}/data" "${BACKUP_FILE}" <<'EOF'
set -e
mkdir -p "$1"
if docker ps --format '{{.Names}}' | grep -qx sub2api; then
  docker exec sub2api sh -c 'PGPASSWORD="$DATABASE_PASSWORD" pg_dump -h "$DATABASE_HOST" -p "$DATABASE_PORT" -U "$DATABASE_USER" -d "$DATABASE_DBNAME"' > "$2"
  [ -s "$2" ] || { echo "[错误] 备份文件为空，中止部署" >&2; exit 1; }
  echo "    完成（$(du -h "$2" | cut -f1)）"
elif docker ps -a --format '{{.Names}}' | grep -qx sub2api; then
  echo "[警告] sub2api 容器存在但未运行，无法 pg_dump，跳过备份" >&2
else
  echo "    未发现现有 sub2api 容器，跳过备份（首次部署）"
fi
EOF
fi

# ---------------------------------------------------------------------------
# 9. 备份 compose 并切换 image: tag（NAS 侧；旧 compose 备份不覆盖）
# ---------------------------------------------------------------------------
COMPOSE_BAK="${COMPOSE_FILE}.bak-$(TZ=Asia/Shanghai date +%Y%m%d-%H%M%S)"
echo "==> compose 已备份: ${SSH_HOST}:${COMPOSE_BAK}"
echo "==> image 行切换: ${IMAGE_NAME}:${OLD_TAG:-?} -> ${FULL_IMAGE}"
rsh bash -s -- "${COMPOSE_FILE}" "${COMPOSE_BAK}" "${IMAGE_NAME}" "${NEW_TAG}" <<'EOF'
set -e
cp -p "$1" "$2"
sed -i -E "s|^([[:space:]]*image:[[:space:]]*)$3:.*|\1$3:$4|" "$1"
grep -qE "^[[:space:]]*image:[[:space:]]*$3:$4([[:space:]]|#|$)" "$1" \
  || { echo "[错误] compose image 行改写失败（原文件备份在 $2），请手工检查" >&2; exit 1; }
grep -nE "^[[:space:]]*image:" "$1" | sed 's/^/    /'
EOF

# ---------------------------------------------------------------------------
# 10. 重建容器（NAS 侧）
# ---------------------------------------------------------------------------
echo "==> 远端 docker compose up -d（容器重建会触发 fnOS SMB 丢失 bug，守护自动恢复）…"
rsh bash -s -- "${COMPOSE_DIR}" <<'EOF'
set -e
cd "$1" && docker compose up -d
EOF

# ---------------------------------------------------------------------------
# 11. 验证：容器 healthy（NAS 侧轮询）+ /health（本机直连 HEALTH_URL）
# ---------------------------------------------------------------------------
echo "==> 等待容器 healthy…"
STATUS="$(rsh bash -s -- <<'EOF'
status=unknown
for _ in $(seq 1 30); do
  status="$(docker inspect -f '{{.State.Health.Status}}' sub2api 2>/dev/null || echo unknown)"
  [ "$status" = healthy ] && break
  [ "$status" = unhealthy ] && break
  sleep 3
done
echo "$status"
EOF
)"

FAIL=0
if [ "${STATUS}" = "healthy" ]; then
  echo "==> 容器状态: Up (healthy)"
else
  echo "[警告] 容器未达 healthy（当前: ${STATUS}）" >&2
  FAIL=1
fi

echo "==> 探测 ${HEALTH_URL}"
HTTP_CODE=""
if command -v curl >/dev/null 2>&1; then
  HTTP_CODE="$(curl -s -o /dev/null -w '%{http_code}' -m 10 "${HEALTH_URL}" || true)"
elif command -v wget >/dev/null 2>&1; then
  if wget -q -T 10 -O /dev/null "${HEALTH_URL}"; then HTTP_CODE=200; fi
fi
if [ "${HTTP_CODE}" = "200" ]; then
  echo "==> /health 返回 200 OK"
else
  echo "[警告] /health 探测失败（HTTP ${HTTP_CODE:-无响应}）" >&2
  FAIL=1
fi

# ---------------------------------------------------------------------------
# 12. 汇总
# ---------------------------------------------------------------------------
echo ""
if [ "${FAIL}" = "0" ]; then
  echo "==> 部署完成: ${FULL_IMAGE} 已上线（${SSH_HOST}）"
else
  echo "==> 部署已执行但验证未全过，请检查: ssh ${SSH_HOST} 'cd ${COMPOSE_DIR} && docker compose logs -f sub2api'" >&2
fi
cat <<EOF
----------------------------------------
本次产物
  镜像     : ${FULL_IMAGE}（NAS 保留作回滚点）
  tar 归档 : ${SSH_HOST}:${REMOTE_TAR_FILE}
  compose  : ${SSH_HOST}:${COMPOSE_BAK}
  本机 tar : $([ "${KEEP_LOCAL_TAR}" = "1" ] && echo "${LOCAL_TAR_FILE}（KEEP_LOCAL_TAR=1 保留）" || echo "已清理（KEEP_LOCAL_TAR=1 可保留）")
回滚方法（验证失败或需退回时）
  1. ssh ${SSH_HOST}
     vi ${COMPOSE_FILE}   # image: 改回 ${IMAGE_NAME}:${OLD_TAG:-<旧tag>}
  2. cd ${COMPOSE_DIR} && docker compose up -d
EOF
if [ -n "${OLD_TAG}" ]; then
cat <<EOF
  或本机一条命令回滚:
     ssh ${SSH_HOST} "sed -i -E 's|^([[:space:]]*image:[[:space:]]*)${IMAGE_NAME}:.*|\1${IMAGE_NAME}:${OLD_TAG}|' ${COMPOSE_FILE} && cd ${COMPOSE_DIR} && docker compose up -d"
EOF
fi
cat <<EOF
  注意：若新版本已执行数据库迁移，回滚前先评估迁移兼容性，必要时先恢复部署前备份
  （${BACKUP_FILE:-备份未启用}）。
----------------------------------------
EOF
[ "${FAIL}" = "0" ] || exit 1
