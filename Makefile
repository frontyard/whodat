# WhoDat — WhatsApp messaging app for Linux
#
# Common targets:
#   make            build ./build/whodat
#   make run        build & run
#   make test       go test ./...
#   make lint       go vet + gofmt check
#   make install    install to PREFIX (default /usr/local)
#   make uninstall
#   make appimage   build dist/whodat-x86_64.AppImage (needs appimagetool)
#   make pkg        build dist/*.deb + dist/*.rpm + dist/*.pkg.tar.zst (needs nfpm)
#   make clean

APP       := whodat
APPID     := io.github.frontyard.whodat
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOFLAGS   := -trimpath
LDFLAGS   := -X main.version=$(VERSION) -s -w
BUILDDIR  := build
DISTDIR   := dist
PREFIX    ?= /usr/local

CGO       := 1
GOTAGS    :=

# Optional local sysroot for Qt6 dev headers + runtime libs — populated by
# ./packaging/qt6-sysroot.sh when qt6-declarative-dev/qt6-multimedia-dev
# can't be installed (no sudo). Not needed when the distro packages exist.
QTSYSROOT_LIBDIR := $(firstword $(wildcard .sysroot-qt/usr/lib/*/pkgconfig))
ifneq ($(QTSYSROOT_LIBDIR),)
export PKG_CONFIG_PATH := $(CURDIR)/$(QTSYSROOT_LIBDIR)$(if $(PKG_CONFIG_PATH),:$(PKG_CONFIG_PATH),)
QTRUN := LD_LIBRARY_PATH=$(CURDIR)/$(dir $(QTSYSROOT_LIBDIR))
endif

.PHONY: all run test lint qmllint fmt tidy install uninstall appimage pkg clean

all: $(BUILDDIR)/$(APP)

$(BUILDDIR)/$(APP): $(shell find cmd internal -type f) go.mod go.sum
	@mkdir -p $(BUILDDIR)
	CGO_ENABLED=$(CGO) go build $(GOFLAGS) -tags '$(GOTAGS)' -ldflags '$(LDFLAGS)' -o $@ ./cmd/whodat

run: $(BUILDDIR)/$(APP)
	$(QTRUN) ./$(BUILDDIR)/$(APP)

test:
	$(QTRUN) go test ./...

lint:
	@test -z "$$(gofmt -l cmd internal | tee /dev/stderr)" || (echo "run 'make fmt'" && exit 1)
	go vet ./...

# QML syntax check — uses the sysroot qmllint when distro tools are absent.
QMLLINT ?= $(firstword $(wildcard .sysroot-qt/usr/lib/qt6/bin/qmllint /usr/lib/qt6/bin/qmllint))
qmllint:
	$(if $(QMLLINT),LD_LIBRARY_PATH=$(CURDIR)/$(dir $(QTSYSROOT_LIBDIR)) $(QMLLINT) internal/qtui/qml/*.qml,@echo "qmllint not found")

fmt:
	gofmt -w cmd internal

tidy:
	go mod tidy

install: $(BUILDDIR)/$(APP)
	install -Dm755 $(BUILDDIR)/$(APP) $(DESTDIR)$(PREFIX)/bin/$(APP)
	install -Dm644 data/$(APPID).desktop $(DESTDIR)$(PREFIX)/share/applications/$(APPID).desktop
	install -Dm644 internal/core/icon.svg $(DESTDIR)$(PREFIX)/share/icons/hicolor/scalable/apps/$(APPID).svg
	install -Dm644 data/$(APPID).metainfo.xml $(DESTDIR)$(PREFIX)/share/metainfo/$(APPID).metainfo.xml
	install -Dm644 NOTICE $(DESTDIR)$(PREFIX)/share/doc/$(APP)/NOTICE
	-update-desktop-database $(DESTDIR)$(PREFIX)/share/applications 2>/dev/null || true
	-gtk-update-icon-cache -qtf $(DESTDIR)$(PREFIX)/share/icons/hicolor 2>/dev/null || true

uninstall:
	rm -f $(DESTDIR)$(PREFIX)/bin/$(APP)
	rm -f $(DESTDIR)$(PREFIX)/share/applications/$(APPID).desktop
	rm -f $(DESTDIR)$(PREFIX)/share/icons/hicolor/scalable/apps/$(APPID).svg
	rm -f $(DESTDIR)$(PREFIX)/share/metainfo/$(APPID).metainfo.xml
	rm -f $(DESTDIR)$(PREFIX)/share/doc/$(APP)/NOTICE

appimage: $(BUILDDIR)/$(APP)
	VERSION=$(VERSION) ./packaging/appimage.sh $(BUILDDIR)/$(APP) $(DISTDIR)/$(APP)-$(VERSION)-x86_64.AppImage

pkg: $(BUILDDIR)/$(APP)
	@mkdir -p $(DISTDIR)
	VERSION=$(VERSION) nfpm package -f packaging/nfpm.yaml --packager deb --target $(DISTDIR)/
	VERSION=$(VERSION) nfpm package -f packaging/nfpm.yaml --packager rpm --target $(DISTDIR)/
	VERSION=$(VERSION) nfpm package -f packaging/nfpm.yaml --packager archlinux --target $(DISTDIR)/

clean:
	rm -rf $(BUILDDIR) $(DISTDIR) AppDir
