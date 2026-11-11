#!/usr/bin/env bash
# Build a WhoDat AppImage.
#
# Usage: packaging/appimage.sh <binary> <output.AppImage>
#
# When linuxdeploy + linuxdeploy-plugin-qt are on PATH we bundle the Qt6
# runtime, platform/imageformat plugins and the QML modules the embedded
# .qml files import — a self-contained image that runs on any host.
# Otherwise we fall back to a thin binary-only AppImage (works on hosts
# that have Qt6 installed, which is every deb/rpm target anyway).
set -euo pipefail

BIN="${1:?usage: appimage.sh <binary> <output>}"
OUT="${2:?usage: appimage.sh <binary> <output>}"
APPDIR="${APPDIR:-AppDir}"
APPIMAGETOOL="${APPIMAGETOOL:-appimagetool}"
ICON=internal/core/icon.svg
ARCH="$(uname -m)"

rm -rf "$APPDIR"
mkdir -p "$APPDIR/usr/bin" "$APPDIR/usr/share/applications" \
         "$APPDIR/usr/share/icons/hicolor/scalable/apps" \
         "$APPDIR/usr/share/doc/whodat"

cp "$BIN" "$APPDIR/usr/bin/whodat"
cp data/io.github.frontyard.whodat.desktop "$APPDIR/usr/share/applications/"
cp "$ICON" "$APPDIR/usr/share/icons/hicolor/scalable/apps/io.github.frontyard.whodat.svg"
cp NOTICE "$APPDIR/usr/share/doc/whodat/"
if [ -f data/io.github.frontyard.whodat.metainfo.xml ]; then
	mkdir -p "$APPDIR/usr/share/metainfo"
	cp data/io.github.frontyard.whodat.metainfo.xml "$APPDIR/usr/share/metainfo/"
fi

# AppDir roots want the desktop file + icon at top level too.
cp data/io.github.frontyard.whodat.desktop "$APPDIR/"
cp "$ICON" "$APPDIR/io.github.frontyard.whodat.svg"

# appimagetool requires a PNG or XPM top-level icon to build .DirIcon —
# render one from the SVG when a converter is available.
PNG="$APPDIR/io.github.frontyard.whodat.png"
if command -v rsvg-convert >/dev/null 2>&1; then
	rsvg-convert -w 256 -h 256 "$ICON" -o "$PNG"
elif command -v magick >/dev/null 2>&1; then
	magick -background none "$ICON" -resize 256x256 "$PNG"
elif command -v convert >/dev/null 2>&1; then
	convert -background none "$ICON" -resize 256x256 "$PNG"
fi
if [ -f "$PNG" ]; then
	mkdir -p "$APPDIR/usr/share/icons/hicolor/256x256/apps"
	cp "$PNG" "$APPDIR/usr/share/icons/hicolor/256x256/apps/"
fi

cat > "$APPDIR/AppRun" <<'EOF'
#!/bin/sh
HERE="$(dirname "$(readlink -f "$0")")"
export LD_LIBRARY_PATH="$HERE/usr/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
exec "$HERE/usr/bin/whodat" "$@"
EOF
chmod +x "$APPDIR/AppRun"

if command -v linuxdeploy >/dev/null 2>&1; then
	# Our QML ships inside the binary via go:embed — point qmlimportscanner
	# at the sources so the qt plugin deploys the modules the UI imports.
	if [ -z "${QML_SOURCES_PATHS:-}" ] && [ -d internal/qtui/qml ]; then
		export QML_SOURCES_PATHS=internal/qtui/qml
	fi
	# Enable the qt plugin when it's on PATH (LINUXDEPLOY_PLUGIN_QT=1 also
	# works for plugin binaries linuxdeploy can't auto-find).
	qt_plugin=()
	if [ -n "${LINUXDEPLOY_PLUGIN_QT:-}" ] || command -v linuxdeploy-plugin-qt >/dev/null 2>&1; then
		qt_plugin=(--plugin qt)
	fi
	# No appimagetool? Have linuxdeploy emit the AppImage itself.
	if ! command -v "$APPIMAGETOOL" >/dev/null 2>&1; then
		export LINUXDEPLOY_OUTPUT_APPIMAGE=1
	fi
	# Qt-native platform theme/style/icon plugins let the bundle pick up the
	# desktop's font & style settings (qt6ct, adwaita) — without them Qt
	# falls back to a generic font. libqgtk3.so is deliberately skipped: it
	# drags the whole GTK3 stack into the image, and every host that selects
	# the gtk3 theme has GTK3 installed by definition.
	qtplugins="$(qmake6 -query QT_INSTALL_PLUGINS 2>/dev/null || qmake -query QT_INSTALL_PLUGINS 2>/dev/null || true)"
	if [ -n "$qtplugins" ]; then
		for so in "$qtplugins"/platformthemes/libqt6ct.so \
		          "$qtplugins"/styles/*.so \
		          "$qtplugins"/iconengines/*.so; do
			[ -f "$so" ] || continue
			cls=$(basename "$(dirname "$so")")
			mkdir -p "$APPDIR/usr/plugins/$cls"
			cp -a "$so" "$APPDIR/usr/plugins/$cls/"
		done
	fi
	linuxdeploy \
		--appdir "$APPDIR" \
		--executable "$APPDIR/usr/bin/whodat" \
		--desktop-file "$APPDIR/usr/share/applications/io.github.frontyard.whodat.desktop" \
		--icon-file "$APPDIR/usr/share/icons/hicolor/scalable/apps/io.github.frontyard.whodat.svg" \
		"${qt_plugin[@]}" \
		${LINUXDEPLOY_OUTPUT_APPIMAGE:+--output appimage}
	# linuxdeploy resolves deps only for files it deploys itself — pull in
	# the full closure of whatever else the copied plugins need (the
	# adwaita/qt6ct helper libs and their deps), skipping core/system libs
	# that must always come from the host. Repeats until nothing new lands.
	queue="$(ls "$APPDIR"/usr/plugins/*/*.so 2>/dev/null || true)"
	while [ -n "$queue" ]; do
		next=""
		for so in $queue; do
			for lib in $(ldd "$so" | awk '/=> \//{print $3}'); do
				b=$(basename "$lib")
				case "$b" in
					libc.so*|libstdc++*|libgcc_s*|libm.so*|libpthread*|libdl.so*|librt.so*|libresolv*|libz.so*|libX11*|libxcb*|libSM.so*|libICE.so*|libXau*|libXdmcp*|libEGL*|libGLX*|libOpenGL*|libGLdispatch*|libfontconfig*|libfreetype*|libexpat*|libuuid*|libcom_err*|libgmp*|libdrm*|libfribidi*|libgtk-3*|libgdk*|libatk*|libepoxy*|libatspi*|libwayland*|libXrender*|libXext*|libXfixes*|libXi.so*|libXrandr*|libXcursor*|libXdamage*|libXcomposite*|libXinerama*|libxkbcommon*|libharfbuzz*|libpng*|libjpeg*|libthai*|libdatrie*|libgraphite*|libpixman*|libmount*|libselinux*|libblkid*) continue ;;
				esac
				if [ ! -e "$APPDIR/usr/lib/$b" ] && [ -f "$lib" ]; then
					# -L: soname files are usually symlinks — copy the real
					# library under the soname, not a dangling link.
					cp -aL "$lib" "$APPDIR/usr/lib/$b" && next="$next $APPDIR/usr/lib/$b"
				fi
			done
		done
		queue="${next# }"
	done
	# linuxdeploy runs every output plugin it finds — its bundled appimage
	# plugin writes <Name>-$VERSION-$ARCH.AppImage to $PWD, where Name comes
	# from the desktop file ("WhoDat"), so don't match a fixed lowercase name.
	img="$(ls -t ./*"$ARCH".AppImage 2>/dev/null | head -n1 || true)"
	if [ -n "$img" ]; then
		mkdir -p "$(dirname "$OUT")"
		mv "$img" "$OUT"
	fi
elif ! command -v "$APPIMAGETOOL" >/dev/null 2>&1; then
	echo "error: neither linuxdeploy nor appimagetool found" >&2
	echo "  install appimagetool: https://github.com/AppImage/AppImageKit/releases" >&2
	exit 1
else
	echo "warning: linuxdeploy not found — building thin AppImage (needs Qt6 on host)" >&2
fi

if [ ! -f "$OUT" ]; then
	if ! command -v "$APPIMAGETOOL" >/dev/null 2>&1; then
		echo "error: no AppImage was produced and appimagetool is not on PATH" >&2
		exit 1
	fi
	mkdir -p "$(dirname "$OUT")"
	"$APPIMAGETOOL" "$APPDIR" "$OUT"
fi
echo "built: $OUT"
