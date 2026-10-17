# Try the app on an iPhone (web preview, no Mac needed)

Dev-only. Serves the Flutter **web** build over HTTPS (self-signed) to Safari on the same network. This is not a native iOS app: for that you need a Mac with Xcode (or a cloud Mac build service).

1. Find your PC's IP on the network the phone is on (`ipconfig`), e.g. `172.20.10.5`.
2. Nothing to do for the certificate: `server.js` generates one for the machine's current addresses (needs `openssl` on the PATH) and regenerates it when they change.
3. Build the web app (from `mobile/`). With no `API_BASE` it talks to whatever address it was opened from, so one build works on any network:
   `flutter build web --release --pwa-strategy=none`
4. Start the API (`:8080`) and the realtime relay (`:3001`) as in the root README, then here: `node server.js`.
5. On the iPhone open `https://<your-ip>:8443`, accept the certificate warning (Show Details -> visit this website).

Why HTTPS: secure storage, WebCrypto (Signal keys), camera and location only work in a secure context; plain `http://<ip>` is not one.
Limits: browser storage instead of the iOS Keychain, no push, no background, performance below native. Windows Firewall must allow inbound TCP 8443 for `node.exe` on your private network.
`*.pem` files are git-ignored: never commit or reuse them.

## Troubleshooting
- **Blank page for a while:** the first load downloads about 4 MB of JavaScript plus a 3 MB wasm file (compressed); on a phone hotspot give it 20-30 s. Later loads are cached.
- **Still blank after a rebuild:** a browser that loaded an older build may keep a service worker/cache. Hard-refresh, or clear site data for the address. (`--pwa-strategy=none` avoids service workers for new builds.)
- **Debug on the PC:** `PREVIEW_HTTP=1 PREVIEW_PORT=8081 node server.js` serves the same build at `http://localhost:8081` (plain HTTP is fine on localhost). Open `https://<your-ip>:8443/healthz` once in the same browser and accept the certificate, because the app's API calls go to the HTTPS address.

## Demo people for Discover
The app sets the test user's location from the device on every launch, so Discover only shows people near *you*. Run `ops/dev/seed_demo_people.sql` (see its header) to place four demo people within a few km of the test user's current location; re-run it whenever you move. Use `SET demo.viewer = '<email>';` first to seed for another account.

## Add to Home Screen (iPhone)
In Safari: Share, then Add to Home Screen. It opens full-screen with the ConnectMe icon. (Browser storage is used, not the iOS Keychain, so this is a preview, not the shipped app.)
