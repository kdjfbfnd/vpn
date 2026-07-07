#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "Please run as root: sudo bash server/scripts/install_android_sdk.sh"
  exit 1
fi

ANDROID_HOME="${ANDROID_HOME:-/opt/android-sdk}"
CMDLINE_VERSION="${CMDLINE_VERSION:-11076708}"
TOOLS_ZIP="/tmp/android-commandlinetools-${CMDLINE_VERSION}.zip"

apt-get update
apt-get install -y ca-certificates curl unzip openjdk-17-jdk

mkdir -p "${ANDROID_HOME}/cmdline-tools"
if [[ ! -x "${ANDROID_HOME}/cmdline-tools/latest/bin/sdkmanager" ]]; then
  curl -L "https://dl.google.com/android/repository/commandlinetools-linux-${CMDLINE_VERSION}_latest.zip" -o "${TOOLS_ZIP}"
  rm -rf "${ANDROID_HOME}/cmdline-tools/latest"
  mkdir -p "${ANDROID_HOME}/cmdline-tools/latest"
  unzip -q "${TOOLS_ZIP}" -d /tmp/android-cmdline
  mv /tmp/android-cmdline/cmdline-tools/* "${ANDROID_HOME}/cmdline-tools/latest/"
  rm -rf /tmp/android-cmdline "${TOOLS_ZIP}"
fi

yes | "${ANDROID_HOME}/cmdline-tools/latest/bin/sdkmanager" --licenses >/dev/null
"${ANDROID_HOME}/cmdline-tools/latest/bin/sdkmanager" \
  "platform-tools" \
  "platforms;android-36" \
  "build-tools;36.0.0"

cat >/etc/profile.d/android-sdk.sh <<EOF
export ANDROID_HOME=${ANDROID_HOME}
export ANDROID_SDK_ROOT=${ANDROID_HOME}
export PATH=\$PATH:${ANDROID_HOME}/platform-tools:${ANDROID_HOME}/cmdline-tools/latest/bin
EOF

echo "Android SDK installed at ${ANDROID_HOME}"
