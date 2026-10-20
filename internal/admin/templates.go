package admin

import (
	"html/template"
	"net/http"
	"time"
)

var funcs = template.FuncMap{
	"ts": func(u int64) string { return time.Unix(u, 0).Format("2006-01-02 15:04:05") },
}

var tmpl = template.Must(template.New("").Funcs(funcs).Parse(pagesHTML))

func render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

const pagesHTML = `
{{define "styles"}}<style>
:root{color-scheme:dark}
*{box-sizing:border-box}
body{margin:0;background:#0d0d0d;color:#e0e0e0;font:14px/1.5 -apple-system,Segoe UI,Roboto,sans-serif}
a{color:#00e676;text-decoration:none}
.wrap{max-width:1000px;margin:0 auto;padding:20px}
.nav{background:#141414;border-bottom:1px solid #262626;padding:12px 20px;display:flex;gap:18px;align-items:center}
.nav .brand{font-weight:700;color:#fff;margin-right:auto}
.card{background:#161616;border:1px solid #262626;border-radius:10px;padding:16px;margin-bottom:16px}
.stats{display:flex;gap:12px;flex-wrap:wrap}
.stat{background:#161616;border:1px solid #262626;border-radius:10px;padding:14px 18px;min-width:120px}
.stat b{display:block;font-size:22px;color:#fff}
.stat span{color:#888;font-size:12px}
table{width:100%;border-collapse:collapse}
th,td{text-align:left;padding:8px 10px;border-bottom:1px solid #222;font-size:13px;vertical-align:top}
th{color:#aaa;font-weight:600}
.ok{color:#00e676}.fail{color:#ff5252}
input,select{background:#1e1e1e;border:1px solid #333;border-radius:6px;color:#fff;padding:8px;font-size:14px}
label{display:block;color:#aaa;margin:10px 0 4px;font-size:13px}
button,.btn{background:#00b248;border:0;color:#001b0c;border-radius:6px;padding:7px 12px;font-size:13px;cursor:pointer}
button.warn{background:#ff8f00;color:#1b1000}
button.danger{background:#c62828;color:#fff}
form.inline{display:inline}
.note{color:#ffb300;font-size:12px}
.muted{color:#888}
</style>{{end}}

{{define "nav"}}<div class="nav">
<span class="brand">SMS Forwarder · Admin</span>
<a href="/admin">Dashboard</a>
<a href="/admin/users">Users</a>
<a href="/admin/logs">Delivery logs</a>
<a href="/admin/data">Data</a>
<a href="/admin/settings">Settings</a>
<a href="/admin/logout">Logout</a>
</div>{{end}}

{{define "login"}}<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>Admin login</title>{{template "styles"}}</head>
<body><div class="wrap" style="max-width:360px">
<h2>Admin login</h2>
{{if .Err}}<p class="fail">Invalid credentials</p>{{end}}
<form method="post" action="/admin/login">
<label>Username</label><input name="username" autofocus style="width:100%">
<label>Password</label><input name="password" type="password" style="width:100%">
<p><button type="submit" style="width:100%;padding:10px">Sign in</button></p>
</form></div></body></html>{{end}}

{{define "dashboard"}}<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>Dashboard</title>{{template "styles"}}</head>
<body>{{template "nav"}}<div class="wrap">
<h2>Overview</h2>
<div class="stats">
<div class="stat"><b>{{.Stats.Users}}</b><span>users</span></div>
<div class="stat"><b>{{.Stats.Channels}}</b><span>channels</span></div>
<div class="stat"><b>{{.Stats.OutboxLen}}</b><span>pending</span></div>
<div class="stat"><b class="ok">{{.Stats.DeliverOK}}</b><span>delivered</span></div>
<div class="stat"><b class="fail">{{.Stats.DeliverNG}}</b><span>failed</span></div>
</div>
<div class="card" style="margin-top:16px">
<h3>Recent delivery logs</h3>
{{template "logtable" .Logs}}
<p><a href="/admin/logs">View all →</a></p>
</div>
</div></body></html>{{end}}

{{define "logtable"}}<table>
<tr><th>Time</th><th>User</th><th>Channel</th><th>Sender</th><th>Status</th><th>Detail</th></tr>
{{range .}}<tr>
<td class="muted">{{ts .CreatedAt}}</td>
<td>{{.UserID}}</td>
<td>{{.ChannelType}}</td>
<td>{{.Sender}}</td>
<td class="{{if eq .Status "ok"}}ok{{else}}fail{{end}}">{{.Status}}</td>
<td class="muted">{{.Detail}}</td>
</tr>{{else}}<tr><td colspan="6" class="muted">no logs</td></tr>{{end}}
</table>{{end}}

{{define "users"}}<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>Users</title>{{template "styles"}}</head>
<body>{{template "nav"}}<div class="wrap">
<h2>Users</h2>
<div class="card"><table>
<tr><th>ID</th><th>Username</th><th>Status</th><th>Channels</th><th>Devices</th><th>Created</th><th>Actions</th></tr>
{{range .Users}}<tr>
<td>{{.ID}}</td><td>{{.Username}}</td>
<td class="{{if eq .Status "active"}}ok{{else}}fail{{end}}">{{.Status}}</td>
<td>{{.Channels}}</td>
<td><a href="/admin/users/devices?user={{.ID}}">{{.Devices}} &rsaquo;</a></td>
<td class="muted">{{ts .CreatedAt}}</td>
<td>
{{if eq .Status "active"}}
<form class="inline" method="post" action="/admin/users/action"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="action" value="ban"><button class="warn">Ban</button></form>
{{else}}
<form class="inline" method="post" action="/admin/users/action"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="action" value="unban"><button>Unban</button></form>
{{end}}
<form class="inline" method="post" action="/admin/users/action"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="action" value="revoke"><button class="warn">Revoke devices</button></form>
<form class="inline" method="post" action="/admin/users/action" onsubmit="return confirm('Delete user {{.Username}} and all their data?')"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="action" value="delete"><button class="danger">Delete</button></form>
</td>
</tr>{{else}}<tr><td colspan="7" class="muted">no users</td></tr>{{end}}
</table></div>
</div></body></html>{{end}}

{{define "devices"}}<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>Devices</title>{{template "styles"}}</head>
<body>{{template "nav"}}<div class="wrap">
<h2>Devices — user {{.UserID}}</h2>
<p class="muted"><a href="/admin/users">&lsaquo; back to users</a> · token values are never stored (only their hash), so they can't be shown — revoke (invalidate token) or delete (remove the row).</p>
<div class="card">
<form method="post" action="/admin/users/devices/action" onsubmit="return confirm('Delete ALL revoked devices of this user?')">
<input type="hidden" name="user" value="{{.UserID}}"><input type="hidden" name="action" value="delete_revoked">
<button class="danger">Delete all revoked</button>
</form>
</div>
<div class="card"><table>
<tr><th>ID</th><th>Label</th><th>Created</th><th>Last seen</th><th>Status</th><th>Action</th></tr>
{{range .Devices}}<tr>
<td>{{.ID}}</td>
<td>{{if .Label}}{{.Label}}{{else}}<span class="muted">—</span>{{end}}</td>
<td class="muted">{{ts .CreatedAt}}</td>
<td class="muted">{{if .LastSeenAt}}{{ts .LastSeenAt}}{{else}}—{{end}}</td>
<td class="{{if .Revoked}}fail{{else}}ok{{end}}">{{if .Revoked}}revoked{{else}}active{{end}}</td>
<td>
{{if not .Revoked}}<form class="inline" method="post" action="/admin/users/devices/action"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="user" value="{{$.UserID}}"><input type="hidden" name="action" value="revoke"><button class="warn">Revoke</button></form>{{end}}
<form class="inline" method="post" action="/admin/users/devices/action" onsubmit="return confirm('Delete device {{.ID}}?')"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="user" value="{{$.UserID}}"><input type="hidden" name="action" value="delete"><button class="danger">Delete</button></form>
</td>
</tr>{{else}}<tr><td colspan="6" class="muted">no devices</td></tr>{{end}}
</table></div>
</div></body></html>{{end}}

{{define "logs"}}<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>Delivery logs</title>{{template "styles"}}</head>
<body>{{template "nav"}}<div class="wrap">
<h2>Delivery logs</h2>
<div class="card">
<form method="get" action="/admin/logs">
<label>Status</label>
<select name="status" onchange="this.form.submit()">
<option value="" {{if eq .Status ""}}selected{{end}}>all</option>
<option value="ok" {{if eq .Status "ok"}}selected{{end}}>ok</option>
<option value="fail" {{if eq .Status "fail"}}selected{{end}}>fail</option>
</select>
<label>User id (0 = any)</label>
<input name="user" value="{{.User}}" style="width:120px">
<button type="submit">Filter</button>
</form>
<div style="margin-top:12px">
<form class="inline" method="post" action="/admin/logs/clear" onsubmit="return confirm('Delete delivery logs older than 30 days?')"><input type="hidden" name="scope" value="30d"><button class="warn">Delete &gt; 30 days old</button></form>
<form class="inline" method="post" action="/admin/logs/clear" onsubmit="return confirm('Delete ALL delivery logs?')"><input type="hidden" name="scope" value="all"><button class="danger">Clear all</button></form>
</div>
</div>
<div class="card">{{template "logtable" .Logs}}</div>
</div></body></html>{{end}}

{{define "data"}}<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>Data</title>{{template "styles"}}</head>
<body>{{template "nav"}}<div class="wrap">
<h2>Data</h2>
<div class="card"><table>
<tr><th>Table</th><th>Rows</th></tr>
<tr><td>users</td><td>{{.C.Users}}</td></tr>
<tr><td>devices</td><td>{{.C.Devices}}</td></tr>
<tr><td>channels</td><td>{{.C.Channels}}</td></tr>
<tr><td>tg_bind_codes</td><td>{{.C.BindCodes}}</td></tr>
<tr><td>outbox (pending deliveries)</td><td>{{.C.Outbox}}</td></tr>
<tr><td>delivery_logs</td><td>{{.C.DeliveryLogs}}</td></tr>
</table></div>
<div class="card">
<h3>Maintenance</h3>
<p class="muted">These tables can accumulate over time; clean them here.</p>
<form class="inline" method="post" action="/admin/data/action" onsubmit="return confirm('Delete used/expired bind codes?')"><input type="hidden" name="action" value="purge_bindcodes"><button class="warn">Purge used/expired bind codes</button></form>
<form class="inline" method="post" action="/admin/data/action" onsubmit="return confirm('Delete ALL delivery logs?')"><input type="hidden" name="action" value="clear_logs"><button class="warn">Clear all delivery logs</button></form>
<form class="inline" method="post" action="/admin/data/action" onsubmit="return confirm('Clear the outbox? Pending, not-yet-delivered messages will be lost.')"><input type="hidden" name="action" value="clear_outbox"><button class="danger">Clear outbox</button></form>
</div>
</div></body></html>{{end}}

{{define "settings"}}<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>Settings</title>{{template "styles"}}</head>
<body>{{template "nav"}}<div class="wrap">
<h2>Settings</h2>
{{if .Saved}}<p class="ok">Saved. Restart the service for a changed bot token to take effect.</p>{{end}}
<div class="card">
<form method="post" action="/admin/settings">
<label>Telegram bot token</label>
<input name="bot_token" value="{{.BotToken}}" style="width:100%" placeholder="from @BotFather">
<label>Telegram bot username (without @)</label>
<input name="bot_username" value="{{.BotUsername}}" style="width:100%" placeholder="YourBot">
<p class="note">These override the BOT_TOKEN / BOT_USERNAME env values. A changed token needs a service restart (the bot starts long-polling at boot).</p>
<label>Broadcast destination (channel/group chat id)</label>
<input name="publish_chat_id" value="{{.PublishChatID}}" style="width:100%" placeholder="e.g. -1001234567890; 0 = off">
<p class="note">A Telegram channel or group where every received SMS is also published, in addition to each user's own delivery. Chat ids are negative for channels/groups (get it via @getidsbot); 0 or empty disables. Changes apply on the next SMS — no restart needed.</p>
<p><button type="submit">Save</button></p>
</form>
</div>
<div class="card">
<h3>Telegram bindings</h3>
<p class="muted">Replaced or renamed the shared bot? Existing bindings point at the old bot and will fail. Reset them so every user re-binds on the new bot (their app will show "not bound"). Note: the server also auto-clears a binding when Telegram reports the user is unreachable (403).</p>
<form method="post" action="/admin/settings/reset-telegram" onsubmit="return confirm('Reset ALL Telegram bindings? Every user must re-bind.')"><button class="danger">Reset all Telegram bindings</button></form>
</div>
</div></body></html>{{end}}
`
