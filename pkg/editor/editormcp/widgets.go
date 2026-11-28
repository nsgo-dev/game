package editormcp

const widgetCSP = "default-src 'none'; img-src data: blob:; style-src 'unsafe-inline'"

const playtestWidgetHTML = `<!doctype html><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data: blob:; style-src 'unsafe-inline'"><title>NSGo playtest report</title><style>body{font:14px system-ui;color:#e5e7eb;background:#111827;padding:16px}h1{font-size:18px}</style><h1>Playtest report</h1><p>This widget displays the structured playtest result supplied by the host.</p>`

const viewportWidgetHTML = `<!doctype html><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data: blob:; style-src 'unsafe-inline'"><title>NSGo viewport</title><style>body{font:14px system-ui;color:#e5e7eb;background:#111827;padding:16px}h1{font-size:18px}</style><h1>Viewport</h1><p>The host supplies the viewport image as tool content.</p>`
