pragma Singleton

import QtQuick

// Bridge wraps the Cmd/Reply property-map channel: Bridge.cmd(op, args, cb)
// sends {id,op,args} to Go and calls cb(ok, data, error) when the matching
// reply arrives. Registered as a singleton via qmldir so every component can
// reach it without threading the instance down the tree.
QtObject {
    property int _seq: 0
    property var _pending: ({})

    function cmd(op, args, cb) {
        var id = ++_seq;
        if (cb) _pending[id] = cb;
        Cmd.payload = JSON.stringify({id: id, op: op, args: args || {}});
        return id;
    }

    // fileUrl converts a filesystem path to a file:// URL, encoding each
    // segment so spaces/#/%/? in filenames don't corrupt the URL.
    function fileUrl(path) {
        if (path === "") return "";
        if (path.indexOf("file://") === 0) return path;
        var segs = path.split("/"), out = [];
        for (var i = 0; i < segs.length; i++)
            out.push(encodeURIComponent(segs[i]));
        return "file://" + out.join("/");
    }

    // Go writes each reply into Reply.payload; a property binding is the
    // reliable trigger — the QQmlPropertyMap valueChanged C++ signal does
    // not reach QML Connections, but its per-key notify (bindings) does.
    property var replyJson: Reply.payload
    onReplyJsonChanged: {
        var r;
        try { r = JSON.parse(replyJson); } catch (e) { return; }
        var cb = _pending[r.id];
        if (cb) {
            delete _pending[r.id];
            cb(r.ok === true, r.data !== undefined ? r.data : {}, r.error || "");
        }
    }
}
