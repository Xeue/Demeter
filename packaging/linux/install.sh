#!/usr/bin/env bash
# Demeter installer: installs the headless server as a systemd service.
#
# Usage (on a Debian-based box, over SSH):
#   sudo ./install.sh                     install or upgrade (auto-detects the
#                                         demeter binary sitting next to this script)
#   sudo ./install.sh --port 9000         listen on a different port
#   sudo ./install.sh --binary ./demeter  use a specific binary
#   sudo ./install.sh uninstall           stop + remove the service and binary (keeps data)
#   sudo ./install.sh uninstall --purge   also delete the data dir and service user
#   sudo ./install.sh set-password        set/reset the admin password (--user NAME, default admin)
#
# It creates a 'demeter' system user, installs the binary to /usr/local/bin,
# writes a hardened systemd unit, stores data in /var/lib/demeter, and (on a
# first install) creates an admin login, prints it, and also saves it to
# /var/lib/demeter/INITIAL_ADMIN_PASSWORD so it can't be lost.
set -euo pipefail

SERVICE=demeter
BIN_DEST=/usr/local/bin/demeter
UNIT=/etc/systemd/system/${SERVICE}.service
DATA_DIR=/var/lib/demeter
RUN_USER=demeter
PORT=8080
BINARY=""
ACTION=install
PURGE=0
PWUSER=admin

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

while [ $# -gt 0 ]; do
	case "$1" in
		install|uninstall|set-password) ACTION="$1" ;;
		--port) PORT="${2:?--port needs a value}"; shift ;;
		--binary) BINARY="${2:?--binary needs a path}"; shift ;;
		--user) PWUSER="${2:?--user needs a name}"; shift ;;
		--purge) PURGE=1 ;;
		-h|--help) awk 'NR==1{next} /^#/{sub(/^# ?/,"");print;next} {exit}' "$0"; exit 0 ;;
		*) echo "unknown argument: $1 (try --help)" >&2; exit 2 ;;
	esac
	shift
done

die() { echo "error: $*" >&2; exit 1; }
need_root() { [ "$(id -u)" = 0 ] || die "run as root, e.g. sudo $0 ${ACTION}"; }
need_systemd() { command -v systemctl >/dev/null 2>&1 || die "systemctl not found; this installer targets systemd distros (Debian/Ubuntu)"; }

# genpass prints a 16-char alphanumeric password. LC_ALL=C avoids "illegal byte
# sequence" in some locales; `|| true` swallows the SIGPIPE that `head` closing
# the pipe early raises under `set -o pipefail`.
genpass() { LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom 2>/dev/null | head -c 16 || true; }

# run a command as the unprivileged service user
run_as() {
	if command -v runuser >/dev/null 2>&1; then
		runuser -u "$RUN_USER" -- "$@"
	else
		su -s /bin/sh "$RUN_USER" -c "$(printf '%q ' "$@")"
	fi
}

find_binary() {
	if [ -n "$BINARY" ]; then
		[ -f "$BINARY" ] || die "binary not found: $BINARY"
		echo "$BINARY"; return
	fi
	local c
	for c in "$SCRIPT_DIR"/demeter "$SCRIPT_DIR"/Demeter-v*-linux-* "$SCRIPT_DIR"/demeter-v*-linux-* "$SCRIPT_DIR"/demeter-linux-*; do
		[ -f "$c" ] && { echo "$c"; return; }
	done
	command -v demeter >/dev/null 2>&1 && { command -v demeter; return; }
	die "no demeter binary found; put it next to this script or pass --binary PATH"
}

write_unit() {
	cat > "$UNIT" <<UNIT
[Unit]
Description=Demeter - bulk programmer for GV UCP/IQ broadcast cards
Documentation=https://github.com/Xeue/Demeter
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${RUN_USER}
Group=${RUN_USER}
ExecStart=${BIN_DEST} --data-dir ${DATA_DIR} --listen :${PORT}
Restart=on-failure
RestartSec=5
StateDirectory=${SERVICE}
StateDirectoryMode=0750
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ProtectKernelTunables=true
ProtectControlGroups=true
RestrictSUIDSGID=true
RestrictRealtime=true
LockPersonality=true

[Install]
WantedBy=multi-user.target
UNIT
}

create_user() {
	id "$RUN_USER" >/dev/null 2>&1 && return
	if command -v useradd >/dev/null 2>&1; then
		useradd --system --no-create-home --shell /usr/sbin/nologin "$RUN_USER"
	else
		adduser --system --no-create-home --group "$RUN_USER" >/dev/null
	fi
}

do_install() {
	need_root; need_systemd
	local src; src="$(find_binary)"
	echo "==> installing $(basename "$src") -> $BIN_DEST  (port :${PORT})"

	create_user

	# Detect a first install BEFORE we create the data dir, so upgrades keep data.
	local first=0
	if [ ! -d "$DATA_DIR" ] || [ -z "$(ls -A "$DATA_DIR" 2>/dev/null)" ]; then first=1; fi

	install -d -o "$RUN_USER" -g "$RUN_USER" -m 0750 "$DATA_DIR"
	install -m 0755 "$src" "$BIN_DEST"
	write_unit
	systemctl daemon-reload

	local admin_user="admin" admin_pass="" generated=0
	if [ "$first" = 1 ]; then
		if [ -t 0 ]; then
			read -r -p "Admin username [admin]: " admin_in || true
			admin_user="${admin_in:-admin}"
			read -r -s -p "Admin password (blank = auto-generate): " admin_pass || true; echo
		fi
		if [ -z "$admin_pass" ]; then
			admin_pass="$(genpass)"
			generated=1
		fi
		run_as "$BIN_DEST" --create-admin "${admin_user}:${admin_pass}" --data-dir "$DATA_DIR"
		if [ "$generated" = 1 ]; then
			# Persist the generated password so it can't be lost if this output scrolls.
			printf 'username: %s\npassword: %s\n' "$admin_user" "$admin_pass" > "$DATA_DIR/INITIAL_ADMIN_PASSWORD"
			chown "$RUN_USER:$RUN_USER" "$DATA_DIR/INITIAL_ADMIN_PASSWORD" 2>/dev/null || true
			chmod 0600 "$DATA_DIR/INITIAL_ADMIN_PASSWORD"
			echo "==> generated an admin password (saved to $DATA_DIR/INITIAL_ADMIN_PASSWORD)"
		fi
	fi

	# enable, then restart (not just start) so an upgrade picks up the new binary.
	systemctl enable "$SERVICE"
	systemctl restart "$SERVICE"

	local ip; ip="$(hostname -I 2>/dev/null | awk '{print $1}')"; [ -n "$ip" ] || ip="<server-ip>"
	echo
	echo "================= Demeter installed ================="
	echo "  URL:     http://${ip}:${PORT}"
	if [ "$first" = 1 ]; then
		echo "  Login:   ${admin_user} / ${admin_pass}"
		[ "$generated" = 1 ] && echo "           (also: sudo cat $DATA_DIR/INITIAL_ADMIN_PASSWORD)"
	else
		echo "  (existing data kept; login unchanged)"
	fi
	echo "  Status:  systemctl status ${SERVICE}"
	echo "  Logs:    journalctl -u ${SERVICE} -f"
	echo "  Set pw:  sudo $0 set-password"
	echo "  Stop:    sudo systemctl stop ${SERVICE}"
	echo "===================================================="
}

# do_set_password sets (or resets) an admin password without a reinstall.
do_set_password() {
	need_root; need_systemd
	[ -d "$DATA_DIR" ] || die "no install found at $DATA_DIR (run install first)"
	local p=""
	if [ -t 0 ]; then read -r -s -p "New password for '${PWUSER}' (blank = auto-generate): " p || true; echo; fi
	if [ -z "$p" ]; then p="$(genpass)"; echo "==> generated a password"; fi
	systemctl stop "$SERVICE" 2>/dev/null || true
	run_as "$BIN_DEST" --create-admin "${PWUSER}:${p}" --data-dir "$DATA_DIR"
	systemctl start "$SERVICE"
	echo "==> password set for '${PWUSER}':  ${p}"
}

do_uninstall() {
	need_root; need_systemd
	systemctl disable --now "$SERVICE" 2>/dev/null || true
	rm -f "$UNIT"
	systemctl daemon-reload
	rm -f "$BIN_DEST"
	echo "==> removed the service and binary"
	if [ "$PURGE" = 1 ]; then
		rm -rf "$DATA_DIR"
		if command -v userdel >/dev/null 2>&1; then userdel "$RUN_USER" 2>/dev/null || true
		else deluser --system "$RUN_USER" 2>/dev/null || true; fi
		echo "==> purged data ($DATA_DIR) and user ($RUN_USER)"
	else
		echo "==> kept your data in $DATA_DIR (re-run with --purge to delete it)"
	fi
}

case "$ACTION" in
	install) do_install ;;
	uninstall) do_uninstall ;;
	set-password) do_set_password ;;
esac
