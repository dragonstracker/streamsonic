# Admin UI — design system and theme contract

The admin SPA is a static bundle embedded in the binary (`internal/webui/assets`).
It is served at `/admin/` and at `/`, with assets under `/admin/assets/`.

## Design tokens

Values below were measured from the reference deployment (Flussonic 26.11) with
`getComputedStyle` on the live admin page, not inferred from screenshots.

### Colour

| Token | Value | Used for |
| --- | --- | --- |
| `--primary` | `#002DAB` | links, active tab underline, primary buttons, stream index |
| `--primary-dark` | `#001F77` | navigation rail background (primary darkened 30%) |
| `--secondary` | `#FF0D70` | accent, active rail marker |
| `--bg` | `#ffffff` | page and panel background |
| `--text` | `#232323` | body text (Material `text.primary`) |
| `--text-secondary` | `#98A2B2` | Material `text.secondary` |
| `--gray` | `#797979` | labels, muted meta |
| `--gray-dark` | `#393939` | strong muted text |
| `--gray-light` | `#B9B9B9` | pipes, disabled |
| `--gray-border` | `#CED4DA` | form control borders |
| `--gray-ultralight` | `#EFEFEF` | inert chips |
| `--panel-border` | `#D2D9DF` | content panel outline |
| `--divider` | `#E0E0E0` | row separators |
| `--tab-bg` | `#F2F4F7` | inactive tab fill |
| `--tab-border` | `#D0D7DD` | inactive tab lower edge |
| `--success` | `#10B981` | online state |
| `--warning` | `#FF9900` | "waiting for clients" |
| `--error` | `#D32F2F` | failures |

### Typography

| Role | Size / weight |
| --- | --- |
| Base | 14px / 400, line-height 1.5 |
| Page title | 21px / 500 |
| Section heading | 14px / 500 |
| Table body, stream rows, meta | 12px / 400 |
| Tab label | 14px, 500 active, 400 inactive |
| Rail label | 10px / 400 |

Font family is **Fira Sans** (SIL OFL 1.1), bundled as latin-subset WOFF2 at
weights 300, 400 and 500, matching the reference `CustomFont` face.

### Metrics

| Token | Value |
| --- | --- |
| Base unit `--dp` | 4px |
| Rail width | 65px |
| Content inset | 84px left, 19px right |
| Page header height | 50px, title top offset 16px |
| Tab height | 34px, padding 6px 16px, radius 8px 8px 0 0 |
| Panel | 1px `#D2D9DF`, padding 24px 16px |
| Breakpoints | xs 0, sm 768, md 900, lg 1200, xl 1536 |

## Route map

| Hash route | Page |
| --- | --- |
| `#/streams` | Media — Streams tab |
| `#/templates` | Media — Templates |
| `#/multiplexers` | Media — Multiplexers |
| `#/sources` | Media — Sources |
| `#/vods` | Media — VODs |
| `#/dvb-cards` | Media — DVB cards |
| `#/streams/<name>` | Stream detail |
| `#/create?entity=stream` | Create stream |
| `#/pulse` | Node health |
| `#/config` | Config — Settings |
| `#/config/editor` | Config — raw editor |
| `#/config/dvrs` | Config — DVR storage |
| `#/config/auth` | Config — editor authorization |
| `#/config/backends` | Config — auth backends |
| `#/config/events` | Config — event sinks |
| `#/cluster` | Cluster |
| `#/iptv/users` | IPTV |
| `#/watcher` | IP Cameras |
| `#/support` | Support |

Stream detail tabs: Overview, Input, Transcoder, DVR, Output, EPG, Auth,
Play sessions.

## Theme contract

`GET /streamer/api/v3/ui_settings` returns the white-label document consumed by
the SPA. It is unauthenticated so the login screen can be branded.

```json
{
  "title": "streamsonic",
  "product": "streamer",
  "brand": "streamsonic",
  "colors": {
    "primary": "#002DAB",
    "secondary": "#FF0D70",
    "background": "#fff"
  },
  "fonts": {
    "regular": "fira-sans-400.woff2",
    "light": "fira-sans-300.woff2",
    "medium": "fira-sans-500.woff2"
  },
  "locales": ["en"],
  "menu_items": {
    "cluster": true,
    "iptv": true,
    "ipcam": true,
    "pulse": true,
    "streamer_config": true
  }
}
```

A deployment re-brands by changing `colors.primary`, `title` and `brand`. The rail
colour derives from `primary` by darkening 30%, so one value drives the whole shell.

## Authentication

The SPA stores `{"login": "...", "password": "..."}` under the localStorage key
`MediaServerAuth` and sends `Authorization: Bearer base64(login:password)` on every
request — the same convention the reference admin uses. The server accepts that
form, HTTP Basic, and a bare bearer token equal to the configured password.
