#!/usr/bin/env bash
# Populate .sysroot-qt/ with Qt6 QML/Multimedia development headers and the
# matching runtime libraries, extracted from Debian packages — for builds on
# machines where `apt install qt6-declarative-dev qt6-multimedia-dev` isn't an
# option (no sudo). When the distro dev packages ARE installed this script and
# the sysroot are unnecessary: pkg-config resolves them system-wide.
#
# Usage: ./packaging/qt6-sysroot.sh
set -euo pipefail
cd "$(dirname "$0")/.."

SR=.sysroot-qt
DEBS=(
	qt6-declarative-dev        # Qt6Qml/Quick headers + .pc files
	qt6-declarative-dev-tools  # qmllint, qmlformat, qmlls
	qt6-multimedia-dev         # Qt6Multimedia headers + .pc files
	libqt6qml6                 # runtime: libQt6Qml.so.6
	libqt6qmlcompiler6         # runtime: qmllint's libQt6QmlCompiler.so.6
	libqt6quick6               # runtime: libQt6Quick.so.6
	libqt6multimedia6          # runtime: libQt6Multimedia.so.6
	libqt6multimediawidgets6   # runtime: libQt6MultimediaWidgets.so.6
)

mkdir -p "$SR/debs"
cd "$SR/debs"
for p in "${DEBS[@]}"; do
	if ! ls "${p}"_*.deb >/dev/null 2>&1; then
		echo "downloading $p"
		apt-get download "$p" || { echo "FAILED: $p (install it via apt instead?)"; exit 1; }
	fi
done
cd ..
for d in debs/*.deb; do dpkg -x "$d" .; done

LIBDIR=usr/lib/x86_64-linux-gnu
PCDIR="$LIBDIR/pkgconfig"

# .pc files hardcode prefix=/usr — point them at the sysroot so pkg-config
# emits -I/-L paths that actually exist.
find "$PCDIR" -name '*.pc' -exec sed -i "s|^prefix=/usr|prefix=$PWD/usr|" {} \;

# Dev packages ship libX.so -> libX.so.6 symlinks; make them resolve inside
# the sysroot (the runtime .debs we extract provide the real files).
find "$LIBDIR" -name 'lib*.so' -type l | while read -r l; do
	tgt="$(readlink "$l")"
	if [ ! -e "$l" ]; then
		base="$(basename "$tgt")"
		if [ -e "$LIBDIR/$base" ]; then
			ln -sf "$base" "$l"
		elif [ -e "/$LIBDIR/$base" ]; then
			ln -sf "/$LIBDIR/$base" "$l"
		else
			echo "warning: unresolved $l -> $tgt"
		fi
	fi
done

cat <<EOF

Qt6 sysroot ready at $(pwd)
Build with:
  export PKG_CONFIG_PATH=$(pwd)/$PCDIR:\$PKG_CONFIG_PATH
  export LD_LIBRARY_PATH=$(pwd)/$LIBDIR:\$LD_LIBRARY_PATH   # only needed if
                                                          # Qt6MultimediaWidgets
                                                          # isn't installed
The top-level Makefile picks these up automatically when $SR exists.
EOF
