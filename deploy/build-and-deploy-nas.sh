#!/usr/bin/env bash
# =============================================================================
# Sub2API（sub2api-so 二开版）NAS 正式实例一键构建 + tar 归档 + 部署脚本
# =============================================================================
# 把 AGENTS.md「NAS 正式实例更新」流程固化为一条命令：
#   1. 构建镜像 sub2api:<上游版本>-so-<日期>（根目录 Dockerfile，注入
#      VERSION/COMMIT/DATE；同日多次构建自动追加 -2/-3…，旧 tag 永不覆盖）
#   2. docker save 导出镜像 tar（+sha256）归档到 /vol2/1000/DockerImages
#   3. 部署前 pg_dump 备份数据库到 <COMPOSE_DIR>/data/
#   4. 备份并改写 /vol2/1000/docker/sub2api/docker-compose.yml 的 image: tag
#   5. docker compose up -d 重建容器，验证 healthy + /health
#
# 用法（NAS 本机或挂 docker.sock 的容器内执行，任意目录）：
#   bash deploy/build-and-deploy-nas.sh              # 全流程（交互确认）
#   bash deploy/build-and-deploy-nas.sh -y           # 跳过确认（脚本化用）
#   TAR_ONLY=1 bash deploy/build-and-deploy-nas.sh   # 只构建 + 出 tar，不部署
#   VERSION=0.2.18 bash deploy/build-and-deploy-nas.sh
#   COMMIT=11e53b1+oc-reasoning-fix bash ...         # 手动指定 commit 展示值
#   GZIP=1 bash ...                                  # tar 压缩为 .tar.gz
#
# 环境变量（均可缺省）：
#   VERSION / COMMIT / TAG（显式指定镜像 tag，跳过同日 -N 递增）/ GZIP /
#   TAR_DIR / COMPOSE_DIR / HEALTH_URL /
#   SKIP_DB_BACKUP=1（跳过部署前备份，不推荐）/ NPM_REGISTRY / GOPROXY / GOSUMDB
#
# 回滚：把 compose 的 image: 改回旧 tag 再 docker compose up -d（旧镜像与
# compose 备份均保留，见脚本结尾打印的指引）。跨版本升级会触发自动数据库
# 迁移，部署前备份默认开启，请勿随意跳过。
#
# 注意：容器重建会触发 fnOS SMB 共享丢失 bug（docker/code/DockerImages 从
# Windows 侧短暂消失），守护脚本 /vol2/1000/docker/fix-smb-shares.sh 数分钟内
# 自动恢复，无需人工干预。
#
# 与 deploy/build-docker-tar.sh 的分工：那个脚本产出「离线分发包」（镜像 tar +
# bundle compose + .env + README），用于全新服务器离线安装；本脚本面向本机
# Compose 正式实例（外部 PG/Redis）的版本更新。
#
# 构建路径用【根目录 Dockerfile】（pnpm@9，与 frontend/pnpm-lock.yaml 的 pnpm 9
# 格式匹配，见 2026-10-21 提交 2a53a42）。注意 deploy/Dockerfile.bundle
# （pnpm@11.9.0）自该次 lockfile 重生成后与 frozen 校验不兼容（pnpm 10+ 视
# package.json#pnpm.overrides 为死配置，只认 pnpm-workspace.yaml#overrides），
# 且缺 resources 权限修复，勿用于 NAS 构建；如需换回可 DOCKERFILE= 覆盖。
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

IMAGE_NAME="${IMAGE_NAME:-sub2api}"
DOCKERFILE="${DOCKERFILE:-${REPO_ROOT}/Dockerfile}"
TAR_DIR="${TAR_DIR:-/vol2/1000/DockerImages}"
COMPOSE_DIR="${COMPOSE_DIR:-/vol2/1000/docker/sub2api}"
COMPOSE_FILE="${COMPOSE_DIR}/docker-compose.yml"
HEALTH_URL="${HEALTH_URL:-http://192.168.88.1:8033/health}"
GOPROXY_ARG="${GOPROXY:-https://goproxy.cn,direct}"
GOSUMDB_ARG="${GOSUMDB:-sum.golang.google.cn}"
GZIP="${GZIP:-0}"
SKIP_DB_BACKUP="${SKIP_DB_BACKUP:-0}"
TAR_ONLY="${TAR_ONLY:-0}"
YES="${YES:-0}"

# ---------------------------------------------------------------------------
# 0. 参数解析
# ---------------------------------------------------------------------------
for arg in "$@"; do
  case "${arg}" in
    -y|--yes) YES=1 ;;
    -h|--help)
      sed -n '2,45p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "[错误] 未知参数: ${arg}（支持 -y/--yes/-h）" >&2
      exit 1
      ;;
  esac
done

# ---------------------------------------------------------------------------
# 1. 前置检查
# ---------------------------------------------------------------------------
command -v docker >/dev/null 2>&1 || { echo "[错误] 未找到 docker 命令" >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo "[错误] docker daemon 不可用" >&2; exit 1; }
[ -f "${DOCKERFILE}" ] || { echo "[错误] 缺少 ${DOCKERFILE}" >&2; exit 1; }
if [ "${TAR_ONLY}" != "1" ]; then
  [ -f "${COMPOSE_FILE}" ] || { echo "[错误] 缺少 ${COMPOSE_FILE}" >&2; exit 1; }
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
# 3. 镜像 tag：<版本>-so-<日期>；同日重复构建自动追加 -2/-3…（旧 tag 不覆盖）
#    TAG= 显式指定时跳过递增探测（重跑补步骤时钉住同一 tag）
# ---------------------------------------------------------------------------
TAG_DATE="$(TZ=Asia/Shanghai date +%Y%m%d)"
if [ -n "${TAG:-}" ]; then
  NEW_TAG="${TAG}"
else
  TAG_BASE="${VERSION}-so-${TAG_DATE}"
  NEW_TAG="${TAG_BASE}"
  SUFFIX=2
  while docker image inspect "${IMAGE_NAME}:${NEW_TAG}" >/dev/null 2>&1; do
    NEW_TAG="${TAG_BASE}-${SUFFIX}"
    SUFFIX=$((SUFFIX + 1))
  done
fi

FULL_IMAGE="${IMAGE_NAME}:${NEW_TAG}"

# 当前线上镜像（用于确认展示与回滚指引）
OLD_TAG=""
if [ "${TAR_ONLY}" != "1" ] && [ -f "${COMPOSE_FILE}" ]; then
  OLD_TAG="$(sed -n -E "s|^[[:space:]]*image:[[:space:]]*${IMAGE_NAME}:([^[:space:]]+).*|\1|p" "${COMPOSE_FILE}" | head -1)"
fi

TAR_FILE="${TAR_DIR}/${IMAGE_NAME}-${NEW_TAG}-docker-image.tar"
[ "${GZIP}" = "1" ] && TAR_FILE="${TAR_FILE}.gz"

# ---------------------------------------------------------------------------
# 4. 打印计划并确认
# ---------------------------------------------------------------------------
echo "==> 版本        : ${VERSION}"
echo "==> 上游基准    : ${BASE_HASH}"
echo "==> 二开 commit : ${COMMIT}"
echo "==> 新镜像      : ${FULL_IMAGE}"
echo "==> tar 归档    : ${TAR_FILE}"
if [ "${TAR_ONLY}" = "1" ]; then
  echo "==> 模式        : 仅构建 + tar 归档（不部署）"
else
  echo "==> 部署目标    : ${COMPOSE_FILE}"
  echo "==> 当前线上    : ${IMAGE_NAME}:${OLD_TAG:-（未读到，首次部署？）}"
  [ "${SKIP_DB_BACKUP}" = "1" ] && echo "==> 数据库备份  : 跳过（SKIP_DB_BACKUP=1）" || echo "==> 数据库备份  : 部署前 pg_dump 到 ${COMPOSE_DIR}/data/"
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
# 5. 构建镜像（根目录 Dockerfile：pnpm@9 与 lockfile 格式匹配；见头注释）
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
# 6. 导出 tar（可选 gzip）+ sha256 校验，归档到 TAR_DIR
#    terminal-buddy 容器内 /vol2/1000/DockerImages 为 ro 挂载，直接写会报
#    read-only file system；此时经临时容器从宿主侧（rw）写入
# ---------------------------------------------------------------------------
TAR_NAME="$(basename "${TAR_FILE}")"
save_tar() {
  if [ "${GZIP}" = "1" ]; then
    docker save "${FULL_IMAGE}" | gzip > "$1"
  else
    docker save -o "$1" "${FULL_IMAGE}"
  fi
}

if touch "${TAR_DIR}/.wtest" 2>/dev/null; then
  rm -f "${TAR_DIR}/.wtest"
  save_tar "${TAR_FILE}"
  ( cd "${TAR_DIR}" && sha256sum "${TAR_NAME}" > "${TAR_NAME}.sha256" )
else
  echo "==> ${TAR_DIR} 本环境只读，经临时容器从宿主侧写入…"
  mkdir -p "${REPO_ROOT}/dist"
  LOCAL_TMP_DIR="${REPO_ROOT}/dist"
  save_tar "${LOCAL_TMP_DIR}/${TAR_NAME}"
  ( cd "${LOCAL_TMP_DIR}" && sha256sum "${TAR_NAME}" > "${TAR_NAME}.sha256" )
  docker run --rm --entrypoint sh \
    -v "${LOCAL_TMP_DIR}:/src:ro" -v "${TAR_DIR}:/dst" \
    "${FULL_IMAGE}" -c "cp /src/${TAR_NAME} /src/${TAR_NAME}.sha256 /dst/"
  rm -f "${LOCAL_TMP_DIR}/${TAR_NAME}" "${LOCAL_TMP_DIR}/${TAR_NAME}.sha256"
fi
echo "==> 镜像已归档: ${TAR_FILE} ($(du -h "${TAR_FILE}" | cut -f1))"
echo "    （校验: cd ${TAR_DIR} && sha256sum -c "$(basename "${TAR_FILE}").sha256"）"

if [ "${TAR_ONLY}" = "1" ]; then
  echo "==> TAR_ONLY=1，流程结束（未部署）。"
  exit 0
fi

# ---------------------------------------------------------------------------
# 7. 部署前数据库备份（旧容器内镜像自带 pg_dump；容器不存在则跳过）
# ---------------------------------------------------------------------------
if [ "${SKIP_DB_BACKUP}" != "1" ]; then
  if docker ps --format '{{.Names}}' | grep -qx sub2api; then
    BACKUP_FILE="${COMPOSE_DIR}/data/backup_predeploy_${TAG_DATE}-$(TZ=Asia/Shanghai date +%H%M%S).sql"
    echo "==> 备份数据库 -> ${BACKUP_FILE}"
    docker exec sub2api sh -c \
      'PGPASSWORD="$DATABASE_PASSWORD" pg_dump -h "$DATABASE_HOST" -p "$DATABASE_PORT" -U "$DATABASE_USER" -d "$DATABASE_DBNAME"' \
      > "${BACKUP_FILE}"
    [ -s "${BACKUP_FILE}" ] || { echo "[错误] 备份文件为空，中止部署" >&2; exit 1; }
    echo "    $(du -h "${BACKUP_FILE}" | cut -f1)"
  elif docker ps -a --format '{{.Names}}' | grep -qx sub2api; then
    echo "[警告] sub2api 容器存在但未运行，无法 pg_dump，跳过备份" >&2
  else
    echo "==> 未发现现有 sub2api 容器，跳过备份（首次部署）"
  fi
fi

# ---------------------------------------------------------------------------
# 8. 备份 compose 并切换 image: tag（旧 compose 备份不覆盖）
# ---------------------------------------------------------------------------
COMPOSE_BAK="${COMPOSE_FILE}.bak-$(TZ=Asia/Shanghai date +%Y%m%d-%H%M%S)"
cp -p "${COMPOSE_FILE}" "${COMPOSE_BAK}"
sed -i -E "s|^([[:space:]]*image:[[:space:]]*)${IMAGE_NAME}:.*|\1${IMAGE_NAME}:${NEW_TAG}|" "${COMPOSE_FILE}"
echo "==> compose 已备份: ${COMPOSE_BAK}"
echo "==> image 行切换: ${IMAGE_NAME}:${OLD_TAG:-?} -> ${FULL_IMAGE}"
grep -nE "^[[:space:]]*image:" "${COMPOSE_FILE}" | sed 's/^/    /'

# ---------------------------------------------------------------------------
# 9. 重建容器
# ---------------------------------------------------------------------------
echo "==> docker compose up -d（容器重建会触发 fnOS SMB 丢失 bug，守护自动恢复）…"
( cd "${COMPOSE_DIR}" && docker compose up -d )

# ---------------------------------------------------------------------------
# 10. 验证：healthy + /health（容器内 + 宿主端口；127.0.0.1 在容器环境指自身，
#     宿主侧探测统一走 192.168.88.1）
# ---------------------------------------------------------------------------
echo "==> 等待容器 healthy…"
HEALTHY=0
for _ in $(seq 1 30); do
  STATUS="$(docker inspect -f '{{.State.Health.Status}}' sub2api 2>/dev/null || echo unknown)"
  if [ "${STATUS}" = "healthy" ]; then HEALTHY=1; break; fi
  if [ "${STATUS}" = "unhealthy" ]; then break; fi
  sleep 3
done

FAIL=0
if [ "${HEALTHY}" = "1" ]; then
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
# 11. 汇总
# ---------------------------------------------------------------------------
echo ""
if [ "${FAIL}" = "0" ]; then
  echo "==> 部署完成: ${FULL_IMAGE} 已上线"
else
  echo "==> 部署已执行但验证未全过，请检查: cd ${COMPOSE_DIR} && docker compose logs -f sub2api" >&2
fi
cat <<EOF
----------------------------------------
本次产物
  镜像     : ${FULL_IMAGE}（保留作回滚点）
  tar 归档 : ${TAR_FILE}
  compose  : ${COMPOSE_BAK}
回滚方法（验证失败或需退回时）
  1. vi ${COMPOSE_FILE}   # image: 改回 ${IMAGE_NAME}:${OLD_TAG:-<旧tag>}
  2. cd ${COMPOSE_DIR} && docker compose up -d
  注意：若新版本已执行数据库迁移，回滚前先评估迁移兼容性，必要时先恢复部署前备份。
----------------------------------------
EOF
[ "${FAIL}" = "0" ] || exit 1
