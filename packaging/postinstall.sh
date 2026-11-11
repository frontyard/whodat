#!/bin/sh
update-desktop-database -q /usr/share/applications 2>/dev/null || true
gtk-update-icon-cache -qtf /usr/share/icons/hicolor 2>/dev/null || true
