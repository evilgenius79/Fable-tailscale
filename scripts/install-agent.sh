#!/bin/sh
# Tailwatch agent installer (POSIX sh).
#
# Installs the tailwatch-agent binary from a GitHub release (or a local file),
# creates an unprivileged system user, writes /etc/tailwatch/agent.env and
# installs + starts a hardened systemd unit.
#
# This script never runs unattended by accident: it prints exactly what it is
# going to do and asks for confirmation. Non-interactive runs must pass --yes.
# Recommended usage is to download, read, then run:
#
#   curl -fsSLO https://raw.githubusercontent.com/evilgenius79/fable-tailscale/main/scripts/install-agent.sh
#   less install-agent.sh
#   sudo sh install-agent.sh --allow-tag tag:tailwatch-hub
#
# Options:
#   --version vX.Y.Z      Release tag to install (default: latest)
#   --binary PATH         Install this local binary instead of downloading
#   --prefix DIR          Install directory (default: /usr/local/bin)
#   --auth MODE           whois | token | both (default: whois, or both when a token is given)
#   --allow-tag TAG       Accept requests from nodes carrying TAG (repeatable)
#   --allow-user LOGIN    Accept requests from nodes owned by LOGIN (repeatable)
#   --allow-node NAME     Accept requests from this MagicDNS node name (repeatable)
#   --opt "FLAGS"         Extra agent flags appended to TAILWATCH_AGENT_OPTS
#   --overwrite-env       Replace an existing /etc/tailwatch/agent.env
#   --require-checksum    Fail when the release has no SHA256SUMS
#   --no-service          Install the binary only (no user, env file or unit)
#   --uninstall           Stop and remove the service and binary (keeps /etc/tailwatch)
#   --print-unit          Print the embedded systemd unit and exit
#   --dry-run             Show the plan and exit
#   --yes                 Do not ask for confirmation
#   -h, --help            This help
#
# Environment:
#   TAILWATCH_AGENT_TOKEN  Shared token written to agent.env (never pass tokens as arguments)

set -eu

REPO="evilgenius79/fable-tailscale"
BIN_NAME="tailwatch-agent"
SVC_NAME="tailwatch-agent"
SVC_USER="tailwatch-agent"
ENV_DIR="/etc/tailwatch"
ENV_FILE="$ENV_DIR/agent.env"
UNIT_PATH="/etc/systemd/system/$SVC_NAME.service"

PREFIX="/usr/local/bin"
VERSION="latest"
LOCAL_BIN=""
AUTH=""
ALLOW_OPTS=""
EXTRA_OPTS=""
OVERWRITE_ENV=0
REQUIRE_SUMS=0
NO_SERVICE=0
UNINSTALL=0
DRY_RUN=0
YES=0

# ---------------------------------------------------------------------------
# Embedded systemd unit. Must stay identical to deploy/systemd/tailwatch-agent.service
# (CI checks this with --print-unit).
# ---------------------------------------------------------------------------
print_unit() {
cat <<'UNIT_EOF'
# Tailwatch agent — hardened systemd unit.
#
# Install:   sudo cp tailwatch-agent.service /etc/systemd/system/
#            sudo systemctl daemon-reload && sudo systemctl enable --now tailwatch-agent
# Configure: /etc/tailwatch/agent.env (mode 0600, root:root). Put the shared
#            token (TAILWATCH_AGENT_TOKEN=...) and extra flags (TAILWATCH_AGENT_OPTS=...)
#            there. See docs/AGENT.md.
# Logs:      journalctl -u tailwatch-agent -f
#
# The agent is read-only: it samples /proc and /sys, asks the local tailscaled
# "who is this?" for every request, and serves JSON on the device's Tailscale
# IP only (port 41820 by default).

[Unit]
Description=Tailwatch agent (per-device metrics for the Tailwatch hub)
Documentation=https://github.com/evilgenius79/fable-tailscale/blob/main/docs/AGENT.md
After=network-online.target tailscaled.service
Wants=network-online.target
Requires=tailscaled.service

[Service]
Type=simple
# Dedicated unprivileged user (created by scripts/install-agent.sh).
# DynamicUser=yes also works: the agent keeps no state.
User=tailwatch-agent
Group=tailwatch-agent
# Uncomment if your tailscaled socket is group-restricted (not the Linux default):
#SupplementaryGroups=tailscale

EnvironmentFile=-/etc/tailwatch/agent.env
ExecStart=/usr/local/bin/tailwatch-agent $TAILWATCH_AGENT_OPTS
Restart=on-failure
RestartSec=5s
TimeoutStopSec=10s
KillSignal=SIGTERM

# --- Filesystem -------------------------------------------------------------
# Nothing is written, ever.
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
# ProtectProc=invisible is deliberately NOT set: the process count metric
# needs to see other users' entries in /proc.
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
# AF_UNIX: tailscaled socket (WhoIs). AF_INET/AF_INET6: the listener.
# AF_NETLINK: interface enumeration (Go's net.Interfaces()) for the
# per-interface counters and Tailscale-IP detection.
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK
SystemCallFilter=@system-service
SystemCallFilter=~@privileged @resources
SystemCallErrorNumber=EPERM
SystemCallArchitectures=native

# --- Resources --------------------------------------------------------------
LimitNOFILE=4096
TasksMax=128
MemoryMax=256M

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

# Reject values that would break the space-separated TAILWATCH_AGENT_OPTS
# string or the env file (whitespace, quotes, backslashes, control chars).
check_word() {
	case "$2" in
		'') die "$1: empty value" ;;
		*[!A-Za-z0-9@._:/+=-]*) die "$1: '$2' contains characters that are not allowed" ;;
	esac
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

# fetch URL DEST — downloads with curl or wget; returns non-zero on 404.
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
		--version)        [ $# -ge 2 ] || die "--version needs a value"; VERSION="$2"; shift 2 ;;
		--binary)         [ $# -ge 2 ] || die "--binary needs a value"; LOCAL_BIN="$2"; shift 2 ;;
		--prefix)         [ $# -ge 2 ] || die "--prefix needs a value"; PREFIX="$2"; shift 2 ;;
		--auth)           [ $# -ge 2 ] || die "--auth needs a value"; AUTH="$2"; shift 2 ;;
		--allow-tag)      [ $# -ge 2 ] || die "--allow-tag needs a value"; check_word --allow-tag "$2"
		                  case "$2" in tag:*) ;; *) die "--allow-tag: '$2' must start with tag:" ;; esac
		                  ALLOW_OPTS="$ALLOW_OPTS --allow-tag $2"; shift 2 ;;
		--allow-user)     [ $# -ge 2 ] || die "--allow-user needs a value"; check_word --allow-user "$2"
		                  ALLOW_OPTS="$ALLOW_OPTS --allow-user $2"; shift 2 ;;
		--allow-node)     [ $# -ge 2 ] || die "--allow-node needs a value"; check_word --allow-node "$2"
		                  ALLOW_OPTS="$ALLOW_OPTS --allow-node $2"; shift 2 ;;
		--opt)            [ $# -ge 2 ] || die "--opt needs a value"; EXTRA_OPTS="$EXTRA_OPTS $2"; shift 2 ;;
		--overwrite-env)  OVERWRITE_ENV=1; shift ;;
		--require-checksum) REQUIRE_SUMS=1; shift ;;
		--no-service)     NO_SERVICE=1; shift ;;
		--uninstall)      UNINSTALL=1; shift ;;
		--print-unit)     print_unit; exit 0 ;;
		--dry-run)        DRY_RUN=1; shift ;;
		--yes|-y)         YES=1; shift ;;
		-h|--help)        usage; exit 0 ;;
		*)                die "unknown option: $1 (see --help)" ;;
	esac
done

case "$AUTH" in
	''|whois|token|both) ;;
	*) die "--auth must be whois, token or both" ;;
esac
case "$VERSION" in
	latest|v[0-9]*) ;;
	*) die "--version must be 'latest' or a tag like v1.2.3" ;;
esac
NL="$(printf '\nx')"; NL="${NL%x}"
case "$EXTRA_OPTS" in
	*[\'\"\\]*|*"$NL"*) die "--opt: quotes, backslashes and newlines are not allowed" ;;
esac

TOKEN="${TAILWATCH_AGENT_TOKEN:-}"
if [ -n "$TOKEN" ]; then
	check_word TAILWATCH_AGENT_TOKEN "$TOKEN"
	[ -n "$AUTH" ] || AUTH="both"
	[ "$AUTH" != whois ] || warn "a token is set but --auth whois ignores it"
fi
[ -n "$AUTH" ] || AUTH="whois"
if [ "$AUTH" != whois ] && [ -z "$TOKEN" ]; then
	die "--auth $AUTH requires TAILWATCH_AGENT_TOKEN in the environment (e.g. TAILWATCH_AGENT_TOKEN=\$(openssl rand -hex 32))"
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
AGENT_OPTS="--auth $AUTH$ALLOW_OPTS$EXTRA_OPTS"
INSTALL_SERVICE=0
if [ "$NO_SERVICE" = 0 ] && [ "$OS" = linux ] && has_systemd; then
	INSTALL_SERVICE=1
fi

# ---------------------------------------------------------------------------
# Uninstall
# ---------------------------------------------------------------------------
if [ "$UNINSTALL" = 1 ]; then
	log "Tailwatch agent uninstall plan:"
	if has_systemd; then
		log "  - systemctl disable --now $SVC_NAME"
		log "  - rm -f $UNIT_PATH && systemctl daemon-reload"
	fi
	log "  - rm -f $DEST"
	log "  - keep $ENV_FILE and the '$SVC_USER' user (remove by hand: rm -rf $ENV_DIR; userdel $SVC_USER)"
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
log "Tailwatch agent install plan ($OS/$ARCH):"
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
		[ -z "$ALLOW_OPTS$EXTRA_OPTS$TOKEN" ] || warn "allow-lists/token given on the command line are ignored while $ENV_FILE exists"
	else
		log "  - write $ENV_FILE (0600 root:root):"
		log "        TAILWATCH_AGENT_OPTS=$AGENT_OPTS"
		[ -z "$TOKEN" ] || log "        TAILWATCH_AGENT_TOKEN=<redacted, ${#TOKEN} chars>"
	fi
	log "  - install $UNIT_PATH (hardened unit, see --print-unit)"
	log "  - systemctl daemon-reload && systemctl enable --now $SVC_NAME"
elif [ "$NO_SERVICE" = 1 ]; then
	log "  - --no-service: no user, env file or unit will be created"
else
	log "  - no systemd on this host: only the binary is installed; see docs/AGENT.md for launchd/rc.d"
fi
if [ "$OS" = linux ] && [ ! -S /var/run/tailscale/tailscaled.sock ]; then
	warn "/var/run/tailscale/tailscaled.sock not found: is tailscaled installed and running? The agent needs it."
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
	log "done. Start it with: $DEST --auth $AUTH$ALLOW_OPTS"
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
		useradd --system --user-group --no-create-home --home-dir /nonexistent --shell "$nologin" "$SVC_USER"
	elif have adduser; then
		addgroup -S "$SVC_USER" 2>/dev/null || true
		adduser -S -D -H -s "$nologin" -G "$SVC_USER" "$SVC_USER"
	else
		die "cannot create user $SVC_USER: no useradd/adduser"
	fi
	log "created user $SVC_USER"
fi

mkdir -p "$ENV_DIR"
chmod 0755 "$ENV_DIR"
if [ ! -f "$ENV_FILE" ] || [ "$OVERWRITE_ENV" = 1 ]; then
	umask 077
	{
		echo "# Tailwatch agent configuration (written by install-agent.sh)."
		echo "# Flags are documented in docs/AGENT.md. Restart after editing:"
		echo "#   sudo systemctl restart $SVC_NAME"
		echo "TAILWATCH_AGENT_OPTS=$AGENT_OPTS"
		if [ -n "$TOKEN" ]; then
			echo "TAILWATCH_AGENT_TOKEN=$TOKEN"
		else
			echo "#TAILWATCH_AGENT_TOKEN="
		fi
	} > "$ENV_FILE.tmp"
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
log "  - ACL:     allow the hub to reach this device on TCP 41820 (see docs/DEPLOY.md)"
log "  - hub:     the hub is accepted when it is owned by the same user, carries tag:tailwatch,"
log "             or matches an --allow-* rule in $ENV_FILE"
