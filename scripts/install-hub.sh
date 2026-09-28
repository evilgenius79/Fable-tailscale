#!/bin/sh
# Tailwatch hub installer (POSIX sh).
#
# Installs the tailwatch hub binary from a GitHub release (or a local file),
# creates the 'tailwatch' system user, writes /etc/tailwatch/hub.env (0600)
# and installs + starts a hardened systemd unit. Data lives in /var/lib/tailwatch.
#
# It prints exactly what it is going to do and asks for confirmation;
# non-interactive runs must pass --yes. Download, read, then run:
#
#   curl -fsSLO https://raw.githubusercontent.com/evilgenius79/fable-tailscale/main/scripts/install-hub.sh
#   less install-hub.sh
#   sudo sh install-hub.sh --admin alice@example.com
#
# Options:
#   --version vX.Y.Z        Release tag to install (default: latest)
#   --binary PATH           Install this local binary instead of downloading
#   --prefix DIR            Install directory (default: /usr/local/bin)
#   --admin LOGIN           Tailscale login granted the admin role (repeatable)
#   --admin-tag TAG         Nodes carrying TAG get the admin role (repeatable)
#   --viewer LOGIN          Restrict viewers to these logins (repeatable; default: any tailnet identity)
#   --viewer-tag TAG        Nodes carrying TAG get the viewer role (repeatable)
#   --enable-admin-actions  Allow device admin actions through the control API
#   --listen ADDR           Listen address (default: auto = Tailscale IPv4:8484)
#   --tailnet NAME          Tailnet name for the control API (default: "-" = key's tailnet)
#   --operator              Run 'tailscale set --operator=tailwatch' so the hub can send disco pings
#   --opt "FLAGS"           Extra hub flags appended to TAILWATCH_OPTS
#   --overwrite-env         Replace an existing /etc/tailwatch/hub.env
#   --require-checksum      Fail when the release has no SHA256SUMS
#   --no-service            Install the binary only (no user, env file or unit)
#   --uninstall             Stop and remove the service and binary (keeps /etc/tailwatch and /var/lib/tailwatch)
#   --print-unit            Print the embedded systemd unit and exit
#   --dry-run               Show the plan and exit
#   --yes                   Do not ask for confirmation
#   -h, --help              This help
#
# Environment (copied into hub.env when set; never pass secrets as arguments):
#   TS_API_KEY                Tailscale API key (tskey-api-...)            [control API]
#   TS_OAUTH_CLIENT_ID        OAuth client id                              [control API, preferred]
#   TS_OAUTH_CLIENT_SECRET    OAuth client secret
#   TAILWATCH_AGENT_TOKEN     Shared token sent to agents (X-Tailwatch-Token)
#   TAILWATCH_WEBHOOK_URL     Generic webhook notifier
#   TAILWATCH_WEBHOOK_SECRET  HMAC secret for the webhook signature
#   TAILWATCH_SLACK_WEBHOOK_URL, TAILWATCH_NTFY_URL, TAILWATCH_NTFY_TOKEN

set -eu

REPO="evilgenius79/fable-tailscale"
BIN_NAME="tailwatch"
SVC_NAME="tailwatch"
SVC_USER="tailwatch"
ENV_DIR="/etc/tailwatch"
ENV_FILE="$ENV_DIR/hub.env"
UNIT_PATH="/etc/systemd/system/$SVC_NAME.service"
DATA_DIR="/var/lib/tailwatch"

PREFIX="/usr/local/bin"
VERSION="latest"
LOCAL_BIN=""
ADMINS=""
ADMIN_TAGS=""
VIEWERS=""
VIEWER_TAGS=""
ADMIN_ACTIONS=0
LISTEN=""
TAILNET=""
OPERATOR=0
EXTRA_OPTS=""
OVERWRITE_ENV=0
REQUIRE_SUMS=0
NO_SERVICE=0
UNINSTALL=0
DRY_RUN=0
YES=0

# ---------------------------------------------------------------------------
# Embedded systemd unit. Must stay identical to deploy/systemd/tailwatch.service
# (CI checks this with --print-unit).
# ---------------------------------------------------------------------------
print_unit() {
cat <<'UNIT_EOF'
# Tailwatch hub — hardened systemd unit.
#
# Install:   sudo cp tailwatch.service /etc/systemd/system/
#            sudo systemctl daemon-reload && sudo systemctl enable --now tailwatch
# Configure: /etc/tailwatch/hub.env (mode 0600, root:root). See deploy/docker/hub.env.example
#            for every TAILWATCH_* / TS_* variable. Extra flags go in TAILWATCH_OPTS.
# Data:      /var/lib/tailwatch (StateDirectory, created by systemd, owned by the service user).
# Logs:      journalctl -u tailwatch -f
#
# The hub only needs: the tailscaled LocalAPI socket (read access is enough for
# status + WhoIs; disco pings need write access, see docs/DEPLOY.md), outbound
# HTTPS to api.tailscale.com (optional) and TCP to agents on the tailnet.

[Unit]
Description=Tailwatch hub (Tailscale network viewer and watchdog)
Documentation=https://github.com/evilgenius79/fable-tailscale
After=network-online.target tailscaled.service
Wants=network-online.target
Requires=tailscaled.service

[Service]
Type=simple
# Dedicated system user (created by scripts/install-hub.sh). To grant it
# write access to tailscaled (needed for on-demand/periodic disco pings):
#   sudo tailscale set --operator=tailwatch
# Alternatively use DynamicUser=yes and set TAILWATCH_PING_INTERVAL=0.
User=tailwatch
Group=tailwatch
# Uncomment if your tailscaled socket is group-restricted (not the Linux default):
#SupplementaryGroups=tailscale

Environment=TAILWATCH_DATA_DIR=/var/lib/tailwatch
Environment=TAILWATCH_LOG_JSON=true
EnvironmentFile=-/etc/tailwatch/hub.env
ExecStart=/usr/local/bin/tailwatch $TAILWATCH_OPTS
Restart=on-failure
RestartSec=5s
TimeoutStopSec=15s
KillSignal=SIGTERM

# --- Filesystem -------------------------------------------------------------
StateDirectory=tailwatch
StateDirectoryMode=0700
# Read-only everything except the state directory. TLS certs from
# `tailscale cert` may live under /etc/tailwatch/tls (readable by the user).
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
ProtectClock=yes
ProtectHostname=yes
ProtectProc=invisible
UMask=0077

# --- Privileges -------------------------------------------------------------
NoNewPrivileges=yes
CapabilityBoundingSet=
AmbientCapabilities=
RestrictSUIDSGID=yes
RestrictRealtime=yes
RestrictNamespaces=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
RemoveIPC=yes

# --- Network / syscalls -----------------------------------------------------
# AF_UNIX: tailscaled socket. AF_INET/AF_INET6: listener, agents, control API.
# AF_NETLINK: Go's net.Interfaces() (used to find the Tailscale IP for
# --listen auto); drop it if you always pass an explicit --listen.
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK
SystemCallFilter=@system-service
SystemCallFilter=~@privileged @resources
SystemCallErrorNumber=EPERM
SystemCallArchitectures=native

# --- Resources --------------------------------------------------------------
LimitNOFILE=65536
TasksMax=512
MemoryMax=1G

[Install]
WantedBy=multi-user.target
UNIT_EOF
}

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
log()  { printf '%s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die()  { printf 'error: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

usage() {
	sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'
}

# Values end up unquoted in an env file / a space-separated flag string.
check_word() {
	case "$2" in
		'') die "$1: empty value" ;;
		*[!A-Za-z0-9@._:/+=%-]*) die "$1: '$2' contains characters that are not allowed" ;;
	esac
}

# Secrets are written single-quoted; they may not contain quotes or newlines.
check_secret() {
	NL="$(printf '\nx')"; NL="${NL%x}"
	case "$2" in
		*\'*|*"$NL"*) die "$1 must not contain single quotes or newlines" ;;
	esac
}

append_list() { # append_list CURRENT VALUE -> comma-separated
	if [ -z "$1" ]; then printf '%s' "$2"; else printf '%s,%s' "$1" "$2"; fi
}

detect_os() {
	case "$(uname -s)" in
		Linux)   echo linux ;;
		Darwin)  echo darwin ;;
		FreeBSD) echo freebsd ;;
		*)       die "unsupported operating system: $(uname -s)" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
		x86_64|amd64)   echo amd64 ;;
		aarch64|arm64)  echo arm64 ;;
		armv7l|armv6l|armhf|arm) echo arm ;;
		*)              die "unsupported architecture: $(uname -m)" ;;
	esac
}

fetch() {
	if have curl; then
		curl -fsSL --proto '=https' --tlsv1.2 --retry 3 -o "$2" "$1"
	elif have wget; then
		wget -q --https-only -O "$2" "$1"
	else
		die "need curl or wget"
	fi
}

sha256_of() {
	if have sha256sum; then
		sha256sum "$1" | awk '{print $1}'
	elif have shasum; then
		shasum -a 256 "$1" | awk '{print $1}'
	elif have openssl; then
		openssl dgst -sha256 "$1" | awk '{print $NF}'
	else
		echo ""
	fi
}

has_systemd() {
	[ -d /run/systemd/system ] && have systemctl
}

confirm() {
	if [ "$YES" = 1 ]; then
		return 0
	fi
	if [ -t 0 ]; then
		printf 'Proceed? [y/N] '
		read -r answer
		case "$answer" in
			y|Y|yes|YES) return 0 ;;
			*) die "aborted" ;;
		esac
	fi
	die "refusing to run non-interactively without --yes (stdin is not a terminal; do not pipe this script into sh)"
}

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------
while [ $# -gt 0 ]; do
	case "$1" in
		--version)     [ $# -ge 2 ] || die "--version needs a value"; VERSION="$2"; shift 2 ;;
		--binary)      [ $# -ge 2 ] || die "--binary needs a value"; LOCAL_BIN="$2"; shift 2 ;;
		--prefix)      [ $# -ge 2 ] || die "--prefix needs a value"; PREFIX="$2"; shift 2 ;;
		--admin)       [ $# -ge 2 ] || die "--admin needs a value"; check_word --admin "$2"
		               ADMINS="$(append_list "$ADMINS" "$2")"; shift 2 ;;
		--admin-tag)   [ $# -ge 2 ] || die "--admin-tag needs a value"; check_word --admin-tag "$2"
		               case "$2" in tag:*) ;; *) die "--admin-tag: '$2' must start with tag:" ;; esac
		               ADMIN_TAGS="$(append_list "$ADMIN_TAGS" "$2")"; shift 2 ;;
		--viewer)      [ $# -ge 2 ] || die "--viewer needs a value"; check_word --viewer "$2"
		               VIEWERS="$(append_list "$VIEWERS" "$2")"; shift 2 ;;
		--viewer-tag)  [ $# -ge 2 ] || die "--viewer-tag needs a value"; check_word --viewer-tag "$2"
		               case "$2" in tag:*) ;; *) die "--viewer-tag: '$2' must start with tag:" ;; esac
		               VIEWER_TAGS="$(append_list "$VIEWER_TAGS" "$2")"; shift 2 ;;
		--enable-admin-actions) ADMIN_ACTIONS=1; shift ;;
		--listen)      [ $# -ge 2 ] || die "--listen needs a value"; check_word --listen "$2"; LISTEN="$2"; shift 2 ;;
		--tailnet)     [ $# -ge 2 ] || die "--tailnet needs a value"; check_word --tailnet "$2"; TAILNET="$2"; shift 2 ;;
		--operator)    OPERATOR=1; shift ;;
		--opt)         [ $# -ge 2 ] || die "--opt needs a value"; EXTRA_OPTS="$EXTRA_OPTS $2"; shift 2 ;;
		--overwrite-env) OVERWRITE_ENV=1; shift ;;
		--require-checksum) REQUIRE_SUMS=1; shift ;;
		--no-service)  NO_SERVICE=1; shift ;;
		--uninstall)   UNINSTALL=1; shift ;;
		--print-unit)  print_unit; exit 0 ;;
		--dry-run)     DRY_RUN=1; shift ;;
		--yes|-y)      YES=1; shift ;;
		-h|--help)     usage; exit 0 ;;
		*)             die "unknown option: $1 (see --help)" ;;
	esac
done

case "$VERSION" in
	latest|v[0-9]*) ;;
	*) die "--version must be 'latest' or a tag like v1.2.3" ;;
esac
NL="$(printf '\nx')"; NL="${NL%x}"
case "$EXTRA_OPTS" in
	*[\'\"\\]*|*"$NL"*) die "--opt: quotes, backslashes and newlines are not allowed" ;;
esac
EXTRA_OPTS="${EXTRA_OPTS# }"

# Secrets and notifier settings from the environment.
SECRET_VARS="TS_API_KEY TS_OAUTH_CLIENT_ID TS_OAUTH_CLIENT_SECRET TAILWATCH_AGENT_TOKEN TAILWATCH_WEBHOOK_URL TAILWATCH_WEBHOOK_SECRET TAILWATCH_SLACK_WEBHOOK_URL TAILWATCH_NTFY_URL TAILWATCH_NTFY_TOKEN"
HAVE_CONTROL_API=0
for v in $SECRET_VARS; do
	val="$(eval "printf '%s' \"\${$v:-}\"")"
	[ -z "$val" ] || check_secret "$v" "$val"
done
if [ -n "${TS_API_KEY:-}" ] || { [ -n "${TS_OAUTH_CLIENT_ID:-}" ] && [ -n "${TS_OAUTH_CLIENT_SECRET:-}" ]; }; then
	HAVE_CONTROL_API=1
fi
if [ "$ADMIN_ACTIONS" = 1 ] && [ "$HAVE_CONTROL_API" = 0 ]; then
	warn "--enable-admin-actions has no effect without TS_API_KEY or TS_OAUTH_CLIENT_ID/SECRET"
fi

OS="$(detect_os)"
ARCH="$(detect_arch)"
ASSET="${BIN_NAME}_${OS}_${ARCH}"
if [ "$VERSION" = latest ]; then
	BASE_URL="https://github.com/$REPO/releases/latest/download"
else
	BASE_URL="https://github.com/$REPO/releases/download/$VERSION"
fi
DEST="$PREFIX/$BIN_NAME"
INSTALL_SERVICE=0
if [ "$NO_SERVICE" = 0 ] && [ "$OS" = linux ] && has_systemd; then
	INSTALL_SERVICE=1
fi

# ---------------------------------------------------------------------------
# Uninstall
# ---------------------------------------------------------------------------
if [ "$UNINSTALL" = 1 ]; then
	log "Tailwatch hub uninstall plan:"
	if has_systemd; then
		log "  - systemctl disable --now $SVC_NAME"
		log "  - rm -f $UNIT_PATH && systemctl daemon-reload"
	fi
	log "  - rm -f $DEST"
	log "  - keep $ENV_FILE, $DATA_DIR and the '$SVC_USER' user"
	log "    (remove by hand: rm -rf $ENV_DIR $DATA_DIR; userdel $SVC_USER)"
	[ "$DRY_RUN" = 0 ] || exit 0
	[ "$(id -u)" = 0 ] || die "run as root (sudo)"
	confirm
	if has_systemd; then
		systemctl disable --now "$SVC_NAME" 2>/dev/null || true
		rm -f "$UNIT_PATH"
		systemctl daemon-reload
	fi
	rm -f "$DEST"
	log "removed."
	exit 0
fi

# ---------------------------------------------------------------------------
# Plan
# ---------------------------------------------------------------------------
write_env() {
	echo "# Tailwatch hub configuration (written by install-hub.sh)."
	echo "# Every setting is documented in README.md and deploy/docker/hub.env.example."
	echo "# This file holds secrets: keep it 0600 root:root. Restart after editing:"
	echo "#   sudo systemctl restart $SVC_NAME"
	echo ""
	echo "# --- Access control ---"
	[ -z "$ADMINS" ]      && echo "#TAILWATCH_ADMINS=alice@example.com"      || echo "TAILWATCH_ADMINS=$ADMINS"
	[ -z "$ADMIN_TAGS" ]  && echo "#TAILWATCH_ADMIN_TAGS=tag:admin"           || echo "TAILWATCH_ADMIN_TAGS=$ADMIN_TAGS"
	[ -z "$VIEWERS" ]     && echo "#TAILWATCH_VIEWERS=*"                      || echo "TAILWATCH_VIEWERS=$VIEWERS"
	[ -z "$VIEWER_TAGS" ] && echo "#TAILWATCH_VIEWER_TAGS=tag:ops"            || echo "TAILWATCH_VIEWER_TAGS=$VIEWER_TAGS"
	[ "$ADMIN_ACTIONS" = 1 ] && echo "TAILWATCH_ENABLE_ADMIN_ACTIONS=true" || echo "#TAILWATCH_ENABLE_ADMIN_ACTIONS=true"
	echo ""
	echo "# --- Listener ---"
	[ -z "$LISTEN" ]  && echo "#TAILWATCH_LISTEN=auto"  || echo "TAILWATCH_LISTEN=$LISTEN"
	echo "#TAILWATCH_TLS_CERT=/etc/tailwatch/tls/hub.crt"
	echo "#TAILWATCH_TLS_KEY=/etc/tailwatch/tls/hub.key"
	echo ""
	echo "# --- Tailscale control API (optional) ---"
	[ -z "$TAILNET" ] && echo "#TAILWATCH_TAILNET=-" || echo "TAILWATCH_TAILNET=$TAILNET"
	for v in TS_API_KEY TS_OAUTH_CLIENT_ID TS_OAUTH_CLIENT_SECRET; do
		val="$(eval "printf '%s' \"\${$v:-}\"")"
		[ -z "$val" ] && echo "#$v=" || echo "$v='$val'"
	done
	echo ""
	echo "# --- Agents ---"
	val="${TAILWATCH_AGENT_TOKEN:-}"
	[ -z "$val" ] && echo "#TAILWATCH_AGENT_TOKEN=" || echo "TAILWATCH_AGENT_TOKEN='$val'"
	echo ""
	echo "# --- Notifications (optional) ---"
	for v in TAILWATCH_WEBHOOK_URL TAILWATCH_WEBHOOK_SECRET TAILWATCH_SLACK_WEBHOOK_URL TAILWATCH_NTFY_URL TAILWATCH_NTFY_TOKEN; do
		val="$(eval "printf '%s' \"\${$v:-}\"")"
		[ -z "$val" ] && echo "#$v=" || echo "$v='$val'"
	done
	echo ""
	echo "# --- Extra flags appended to the command line ---"
	[ -z "$EXTRA_OPTS" ] && echo "#TAILWATCH_OPTS=--poll-interval 30s" || echo "TAILWATCH_OPTS=$EXTRA_OPTS"
}

log "Tailwatch hub install plan ($OS/$ARCH):"
if [ -n "$LOCAL_BIN" ]; then
	[ -f "$LOCAL_BIN" ] || die "--binary: $LOCAL_BIN does not exist"
	log "  - install local binary $LOCAL_BIN -> $DEST (0755, root)"
else
	log "  - download $BASE_URL/$ASSET"
	if [ "$REQUIRE_SUMS" = 1 ]; then
		log "  - verify it against $BASE_URL/SHA256SUMS (required)"
	else
		log "  - verify it against $BASE_URL/SHA256SUMS when present"
	fi
	log "  - install to $DEST (0755, root)"
fi
if [ "$INSTALL_SERVICE" = 1 ]; then
	log "  - create system user '$SVC_USER' (no home, no login) if missing"
	if [ -f "$ENV_FILE" ] && [ "$OVERWRITE_ENV" = 0 ]; then
		log "  - keep existing $ENV_FILE (pass --overwrite-env to replace it)"
	else
		log "  - write $ENV_FILE (0600 root:root) with:"
		write_env | grep -E '^[A-Z]' | sed -E "s/^(TS_OAUTH_CLIENT_SECRET|TS_API_KEY|TAILWATCH_AGENT_TOKEN|TAILWATCH_WEBHOOK_SECRET|TAILWATCH_NTFY_TOKEN|TAILWATCH_NTFY_URL|TAILWATCH_[A-Z_]*WEBHOOK_URL)=.*/\1=<redacted>/" | sed 's/^/        /'
	fi
	log "  - install $UNIT_PATH (hardened unit, see --print-unit); data in $DATA_DIR"
	[ "$OPERATOR" = 0 ] || log "  - tailscale set --operator=$SVC_USER (lets the hub send disco pings)"
	log "  - systemctl daemon-reload && systemctl enable --now $SVC_NAME"
elif [ "$NO_SERVICE" = 1 ]; then
	log "  - --no-service: no user, env file or unit will be created"
else
	log "  - no systemd on this host: only the binary is installed; see docs/DEPLOY.md"
fi
if [ "$OS" = linux ] && [ ! -S /var/run/tailscale/tailscaled.sock ]; then
	warn "/var/run/tailscale/tailscaled.sock not found: is tailscaled installed and running? The hub needs it."
fi
[ "$DRY_RUN" = 0 ] || exit 0
[ "$(id -u)" = 0 ] || die "run as root (sudo); use --dry-run to preview"
confirm

# ---------------------------------------------------------------------------
# Install binary
# ---------------------------------------------------------------------------
TMP="$(mktemp -d 2>/dev/null || mktemp -d -t tailwatch)"
trap 'rm -rf "$TMP"' EXIT INT TERM

if [ -n "$LOCAL_BIN" ]; then
	cp "$LOCAL_BIN" "$TMP/$ASSET"
else
	log "downloading $ASSET ..."
	fetch "$BASE_URL/$ASSET" "$TMP/$ASSET" || die "download failed: $BASE_URL/$ASSET"
	if fetch "$BASE_URL/SHA256SUMS" "$TMP/SHA256SUMS" 2>/dev/null; then
		expected="$(awk -v f="$ASSET" '{ n=$2; sub(/^\*/, "", n); if (n == f) print $1 }' "$TMP/SHA256SUMS" | head -n 1)"
		actual="$(sha256_of "$TMP/$ASSET")"
		[ -n "$expected" ] || die "SHA256SUMS does not list $ASSET"
		[ -n "$actual" ] || die "no sha256 tool available (sha256sum, shasum or openssl) to verify the download"
		[ "$expected" = "$actual" ] || die "checksum mismatch for $ASSET: expected $expected, got $actual"
		log "checksum OK ($actual)"
	elif [ "$REQUIRE_SUMS" = 1 ]; then
		die "SHA256SUMS not available for this release (--require-checksum)"
	else
		warn "no SHA256SUMS published for this release; download not verified"
	fi
fi

chmod 0755 "$TMP/$ASSET"
mkdir -p "$PREFIX"
if have install; then
	install -m 0755 "$TMP/$ASSET" "$DEST"
else
	cp "$TMP/$ASSET" "$DEST" && chmod 0755 "$DEST"
fi
log "installed $DEST"

if [ "$INSTALL_SERVICE" = 0 ]; then
	log "done. Try it: $DEST --demo --listen 127.0.0.1:8484 --data-dir ./data-demo"
	exit 0
fi

# ---------------------------------------------------------------------------
# User, env file, unit
# ---------------------------------------------------------------------------
if ! getent passwd "$SVC_USER" >/dev/null 2>&1; then
	nologin=/usr/sbin/nologin
	[ -x "$nologin" ] || nologin=/sbin/nologin
	[ -x "$nologin" ] || nologin=/bin/false
	if have useradd; then
		useradd --system --user-group --no-create-home --home-dir "$DATA_DIR" --shell "$nologin" "$SVC_USER"
	elif have adduser; then
		addgroup -S "$SVC_USER" 2>/dev/null || true
		adduser -S -D -H -h "$DATA_DIR" -s "$nologin" -G "$SVC_USER" "$SVC_USER"
	else
		die "cannot create user $SVC_USER: no useradd/adduser"
	fi
	log "created user $SVC_USER"
fi

mkdir -p "$ENV_DIR"
chmod 0755 "$ENV_DIR"
if [ ! -f "$ENV_FILE" ] || [ "$OVERWRITE_ENV" = 1 ]; then
	umask 077
	write_env > "$ENV_FILE.tmp"
	chmod 0600 "$ENV_FILE.tmp"
	mv "$ENV_FILE.tmp" "$ENV_FILE"
	umask 022
	log "wrote $ENV_FILE"
fi

umask 022
print_unit > "$UNIT_PATH.tmp"
chmod 0644 "$UNIT_PATH.tmp"
mv "$UNIT_PATH.tmp" "$UNIT_PATH"
systemctl daemon-reload

if [ "$OPERATOR" = 1 ]; then
	if have tailscale; then
		tailscale set --operator="$SVC_USER" || warn "tailscale set --operator failed; pings will be unavailable (set TAILWATCH_PING_INTERVAL=0)"
	else
		warn "tailscale CLI not found; skipping --operator"
	fi
fi

if systemctl is-active --quiet "$SVC_NAME"; then
	systemctl restart "$SVC_NAME"
	log "restarted $SVC_NAME"
else
	systemctl enable --now "$SVC_NAME"
	log "enabled and started $SVC_NAME"
fi

log ""
log "Next steps:"
log "  - check:   systemctl status $SVC_NAME ; journalctl -u $SVC_NAME -n 20"
log "  - open:    http://<hub-tailscale-ip>:8484/ from any device on your tailnet"
log "  - ACL:     let members reach the hub on TCP 8484 and the hub reach agents on TCP 41820 (docs/DEPLOY.md)"
log "  - agents:  scripts/install-agent.sh on each device you want CPU/memory/disk metrics from (docs/AGENT.md)"
[ "$OPERATOR" = 1 ] || log "  - pings:   sudo tailscale set --operator=$SVC_USER  (or set TAILWATCH_PING_INTERVAL=0 in $ENV_FILE)"
