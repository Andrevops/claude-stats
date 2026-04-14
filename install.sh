#!/bin/sh
set -e

REPO="AgusRdz/claude-stats"

# Detect OS
OS="$(uname -s)"
case "$OS" in
  Linux*)              OS="linux"   ;;
  Darwin*)             OS="darwin"  ;;
  MINGW*|MSYS*|CYGWIN*) OS="windows" ;;
  *) echo "unsupported OS: $OS" >&2; exit 1 ;;
esac

# Set default install dir
if [ -z "$CLAUDE_STATS_INSTALL_DIR" ]; then
  if [ "$OS" = "windows" ]; then
    INSTALL_DIR="$(cygpath "$LOCALAPPDATA/Programs/claude-stats" 2>/dev/null || echo "$HOME/AppData/Local/Programs/claude-stats")"
  else
    INSTALL_DIR="$HOME/.local/bin"
  fi
else
  INSTALL_DIR="$CLAUDE_STATS_INSTALL_DIR"
fi

# Detect architecture
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

EXT=""
if [ "$OS" = "windows" ]; then
  EXT=".exe"
fi

BINARY="claude-stats-${OS}-${ARCH}${EXT}"

# Get latest version
if [ -z "$CLAUDE_STATS_VERSION" ]; then
  CLAUDE_STATS_VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' \
    | sed 's/.*"tag_name": *"//;s/".*//')
fi

if [ -z "$CLAUDE_STATS_VERSION" ]; then
  echo "failed to determine latest version" >&2
  exit 1
fi

URL="https://github.com/${REPO}/releases/download/${CLAUDE_STATS_VERSION}/${BINARY}"

echo "installing claude-stats ${CLAUDE_STATS_VERSION} (${OS}/${ARCH})..."

mkdir -p "$INSTALL_DIR"
TMPBIN=$(mktemp "${INSTALL_DIR}/claude-stats.XXXXXX")
curl -fsSL "$URL" -o "$TMPBIN"
chmod +x "$TMPBIN"
mv -f "$TMPBIN" "${INSTALL_DIR}/claude-stats${EXT}"

echo "installed claude-stats to ${INSTALL_DIR}/claude-stats${EXT}"
echo ""

# Add to PATH if not already present
case ":$PATH:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    if [ "$OS" = "windows" ]; then
      WIN_DIR=$(cygpath -w "$INSTALL_DIR" 2>/dev/null || echo "$INSTALL_DIR")
      powershell.exe -NoProfile -Command \
        "\$p = [Environment]::GetEnvironmentVariable('Path', 'User'); \
         \$d = '${WIN_DIR}'.TrimEnd('\\\\'); \
         if ((\$p -split ';' | ForEach-Object { \$_.TrimEnd('\\\\') }) -notcontains \$d) { \
           [Environment]::SetEnvironmentVariable('Path', \"\$d;\$p\", 'User'); \
           Write-Host \"Added \$d to User PATH\" \
         }"
      export PATH="${INSTALL_DIR}:$PATH"
    else
      SHELL_NAME="$(basename "${SHELL:-}")"
      case "$SHELL_NAME" in
        zsh)  SHELL_RC="$HOME/.zshrc" ;;
        bash) SHELL_RC="$HOME/.bashrc" ;;
        *)    SHELL_RC="" ;;
      esac

      PATH_LINE="export PATH=\"${INSTALL_DIR}:\$PATH\""

      if [ -n "$SHELL_RC" ]; then
        if ! grep -qF "$INSTALL_DIR" "$SHELL_RC" 2>/dev/null; then
          printf '\n# claude-stats\n%s\n' "$PATH_LINE" >> "$SHELL_RC"
          echo "added ${INSTALL_DIR} to PATH in $SHELL_RC"
          echo "reload your shell: source $SHELL_RC"
        fi
      else
        echo "NOTE: add ${INSTALL_DIR} to your PATH:"
        echo "  $PATH_LINE"
      fi
      export PATH="${INSTALL_DIR}:$PATH"
      echo ""
    fi
    ;;
esac

echo "done! run: claude-stats"
