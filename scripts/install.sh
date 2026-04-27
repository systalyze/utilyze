#!/bin/sh

BIN_NAME="utlz"
ALT_BIN_NAME="utilyze"
VERSION="${UTLZ_VERSION:-latest}"
USER_ID="$(id -u)"
SYSTEM_INSTALL_DIR="/usr/local/bin"

if [ -n "${UTLZ_INSTALL_DIR:-}" ]; then
  INSTALL_DIR="${UTLZ_INSTALL_DIR}"
elif [ -n "${HOME:-}" ]; then
  INSTALL_DIR="${HOME}/.local/bin"
else
  echo "Error: HOME is not set. Set UTLZ_INSTALL_DIR to the directory you want to install ${BIN_NAME} to." >&2
  exit 1
fi

case "$(uname -s)" in
  Linux)  OS=linux ;;
  Darwin) OS=darwin ;;
  *) echo "Error: unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "Error: unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

command -v curl >/dev/null 2>&1 || {
  echo "Error: curl is required." >&2
  exit 1
}

asset_name="${BIN_NAME}-${OS}-${ARCH}"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' 0

if [ "$VERSION" = "latest" ]; then
  url="https://github.com/systalyze/utilyze/releases/latest/download/${asset_name}"
else
  url="https://github.com/systalyze/utilyze/releases/download/${VERSION}/${asset_name}"
fi

echo "Downloading ${asset_name}..."
curl -fsSL "$url" -o "$tmpdir/$BIN_NAME" || {
  echo "Error: download failed." >&2
  exit 1
}

mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmpdir/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME"

if [ -e "$INSTALL_DIR/$ALT_BIN_NAME" ] && [ ! -L "$INSTALL_DIR/$ALT_BIN_NAME" ]; then
  echo "Warning: skipping ${ALT_BIN_NAME} symlink because ${INSTALL_DIR}/${ALT_BIN_NAME} already exists." >&2
else
  rm -f "$INSTALL_DIR/$ALT_BIN_NAME"
  ln -s "$BIN_NAME" "$INSTALL_DIR/$ALT_BIN_NAME"
fi

INSTALLED_VERSION="${VERSION}"
if v="$("${INSTALL_DIR}/${BIN_NAME}" -version 2>/dev/null)"; then
  INSTALLED_VERSION="$v"
fi
echo "Installed ${BIN_NAME} and ${ALT_BIN_NAME} to ${INSTALL_DIR} (${INSTALLED_VERSION})"

run_path="${INSTALL_DIR}/${BIN_NAME}"
setcap=0
install_system_wide=0
needs_sudo_for_profiling=0
[ "$OS" = "linux" ] && needs_sudo_for_profiling=1

run_priv() {
  if [ "$USER_ID" -eq 0 ] && ! command -v sudo >/dev/null 2>&1; then
    "$@"
  else
    sudo "$@"
  fi
}

symlink_system_wide() {
  ok=0
  for n in "$BIN_NAME" "$ALT_BIN_NAME"; do
    run_priv ln -sf "${INSTALL_DIR}/${n}" "${SYSTEM_INSTALL_DIR}/${n}" || ok=1
  done
  return "$ok"
}

print_symlink_commands() {
  echo "  sudo ln -sf \"${INSTALL_DIR}/${BIN_NAME}\" ${SYSTEM_INSTALL_DIR}/${BIN_NAME}" >&2
  echo "  sudo ln -sf \"${INSTALL_DIR}/${ALT_BIN_NAME}\" ${SYSTEM_INSTALL_DIR}/${ALT_BIN_NAME}" >&2
}

path_contains_dir() {
  dir="${1%/}"
  old_ifs="$IFS"
  IFS=:
  for entry in ${PATH:-}; do
    IFS="$old_ifs"
    entry="${entry%/}"
    if [ "$entry" = "$dir" ]; then
      return 0
    fi
    IFS=:
  done
  IFS="$old_ifs"
  return 1
}

shell_quote() {
  printf "%s" "$1" | sed "s/'/'\\\\''/g; s/^/'/; s/$/'/"
}

shell_profile() {
  if [ -n "${UTLZ_PATH_FILE:-}" ]; then
    printf "%s\n" "$UTLZ_PATH_FILE"
    return
  fi
  if [ -z "${HOME:-}" ]; then
    return 1
  fi

  case "${SHELL##*/}" in
    zsh)  printf "%s\n" "${HOME}/.zshrc" ;;
    bash)
      if [ "$OS" = "darwin" ]; then
        printf "%s\n" "${HOME}/.bash_profile"
      else
        printf "%s\n" "${HOME}/.bashrc"
      fi
      ;;
    *)    printf "%s\n" "${HOME}/.profile" ;;
  esac
}

add_install_dir_to_path() {
  profile="$(shell_profile)" || return 1
  quoted_install_dir="$(shell_quote "$INSTALL_DIR")"

  if [ -f "$profile" ] && grep -F "$INSTALL_DIR" "$profile" >/dev/null 2>&1; then
    PATH="${INSTALL_DIR}:${PATH:-}"
    export PATH
    echo "${INSTALL_DIR} is already configured in ${profile}."
    return 0
  fi

  {
    echo
    echo "# Added by utlz installer"
    echo "export PATH=${quoted_install_dir}:\$PATH"
  } >>"$profile" || return 1

  PATH="${INSTALL_DIR}:${PATH:-}"
  export PATH
  echo "Added ${INSTALL_DIR} to PATH in ${profile}. Restart your shell to pick up the change:"
  echo
  echo "  source ${profile}"
  echo
}

if [ "$OS" = "linux" ] && [ -r /dev/tty ] && [ -w /dev/tty ]; then
  if [ -z "${UTLZ_INSTALL_WITHOUT_ROOT:-}" ]; then
    if [ "$USER_ID" -ne 0 ]; then
      echo >&2
      echo "Warning: ${BIN_NAME} usually needs sudo for profiling capabilities and is best installed system-wide." >&2
      printf 'Would you like to install %s system-wide to %s? [y/N] ' "$BIN_NAME" "$SYSTEM_INSTALL_DIR" >&2
      if read -r answer </dev/tty; then
        case "$answer" in y|Y|[Yy][Ee][Ss]) install_system_wide=1 ;; esac
      else
        echo "Error: could not read confirmation." >&2
      fi
    else
      echo "Running installer as root, also installing ${BIN_NAME} system-wide to ${SYSTEM_INSTALL_DIR}."
      install_system_wide=1
    fi

    if [ "$USER_ID" -ne 0 ] && ! command -v sudo >/dev/null 2>&1; then
      echo "Unable to install system-wide: sudo is not available. Run ${BIN_NAME} as root from ${INSTALL_DIR}/${BIN_NAME}" \
        "or add symlinks under ${SYSTEM_INSTALL_DIR} as root." >&2
      install_system_wide=0
    elif [ "$install_system_wide" -eq 1 ]; then
      if symlink_system_wide; then
        echo "Installed ${BIN_NAME} and ${ALT_BIN_NAME} to ${SYSTEM_INSTALL_DIR}"
        run_path="${BIN_NAME}"
      else
        echo "Error: system-wide symlinks failed. You can add them manually with:" >&2
        print_symlink_commands
      fi
    else
      echo "Skipping system-wide install. For 'sudo ${BIN_NAME}' to resolve from PATH, add:" >&2
      echo >&2
      print_symlink_commands
      echo >&2
    fi
  fi

  if command -v setcap >/dev/null 2>&1; then
    printf 'Would you like to set CAP_SYS_ADMIN on %s for non-root profiling? [y/N] ' "$BIN_NAME" >&2
    if read -r answer </dev/tty; then
      case "$answer" in y|Y|[Yy][Ee][Ss]) setcap=1 ;; esac
    else
      echo "Error: could not read confirmation." >&2
    fi

    if [ "$setcap" -eq 1 ]; then
      if run_priv setcap cap_sys_admin+ep "${INSTALL_DIR}/${BIN_NAME}"; then
        echo "Set CAP_SYS_ADMIN capability for ${BIN_NAME}."
        needs_sudo_for_profiling=0
      else
        setcap=0
        echo "Error: setcap failed. You will need to run with sudo when profiling or retry setcap as root." >&2
      fi
    else
      echo "Did not set capability."
    fi
  fi
fi

install_dir_on_path=0
if path_contains_dir "$INSTALL_DIR"; then
  install_dir_on_path=1
elif [ -z "${UTLZ_INSTALL_WITHOUT_PATH:-}" ]; then
  if [ -r /dev/tty ] && [ -w /dev/tty ]; then
    printf 'Would you like to add %s to your PATH? [Y/n] ' "$INSTALL_DIR" >&2
    if read -r answer </dev/tty; then
      case "$answer" in
        n|N|[Nn][Oo]) ;;
        *)
          if add_install_dir_to_path; then
            install_dir_on_path=1
          else
            echo "Warning: failed to add ${INSTALL_DIR} to PATH. Add it manually to run ${BIN_NAME} from any terminal." >&2
          fi
          ;;
      esac
    else
      echo "Warning: could not read confirmation. Add ${INSTALL_DIR} to PATH to run ${BIN_NAME} from any terminal." >&2
    fi
  else
    echo "Add ${INSTALL_DIR} to PATH to run ${BIN_NAME} from any terminal." >&2
  fi
fi

if [ "$needs_sudo_for_profiling" -eq 1 ]; then
  if [ "$run_path" = "${BIN_NAME}" ]; then
    run_hint="sudo ${BIN_NAME}"
  else
    run_hint="sudo ${INSTALL_DIR}/${BIN_NAME}"
  fi
else
  if [ "$run_path" = "${BIN_NAME}" ] || [ "$install_dir_on_path" -eq 1 ]; then
    run_hint="${BIN_NAME}"
  else
    run_hint="${INSTALL_DIR}/${BIN_NAME}"
  fi
fi

echo "✅ Successfully installed. Start monitoring your GPUs by running '${run_hint}'."
