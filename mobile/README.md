# ConnectMe mobile (Flutter)

## Run

```bash
flutter pub get
# Android emulator reaches the host as 10.0.2.2 (the default). Physical device: use your LAN IP.
flutter run --dart-define=API_BASE=http://10.0.2.2:8080 --dart-define=REALTIME_BASE=http://10.0.2.2:3001
```

Backend: `docker compose up -d` in the repo root, then `go run ./cmd/server` in `backend/` and `npm start` in `realtime/`.

## Verify

```bash
flutter analyze
flutter test            # unit + widget + E2E crypto tests
flutter build web --release   # whole-app compile check (no Android SDK needed)
```

## Layout

- `lib/core` — config, theme, router (+guard), network (Dio client with single-flight refresh), models
- `lib/features/*` — auth, discovery, hotel_mode, group_dates, chat (+ `crypto/` Signal), profile, payments, live (list, watch, broadcast; `media_session.dart` is the only file that touches the LiveKit SDK)
- `lib/shared/widgets` — button, avatar, loading/empty/error states

## Security notes

- Tokens and all key material are in the platform keystore; keys are namespaced per user.
- Chat history exists only on the device that decrypted it; reinstalling loses it.
- Android/iOS release builds additionally need signing config, store listings, and the in-app-purchase decision.

## Live streaming

- Needs the API started with `LIVEKIT_URL` set (see the root README). Without it the `/live` routes do not exist and the Live screen shows an error.
- Camera, microphone, location and photo permissions are declared in `android/app/src/main/AndroidManifest.xml` and `ios/Runner/Info.plist`. They are untested on a physical device.
- Widget tests use a fake `LiveMedia`, so they never touch WebRTC; the real media path needs a device and a LiveKit server.

## Design system

- Tokens: `lib/core/theme/tokens.dart` (palette, the three mode lights, radii, font names). Theme: `lib/core/theme/app_theme.dart`.
- Rules of thumb: colour with meaning belongs to the visibility modes; everything else stays porcelain and ink. Gradients appear only where they encode a mode. Use `ModeLight.of(mode)` instead of picking colours by hand.
- Fonts (Gloock, Figtree) are bundled in `assets/fonts` under the SIL OFL; do not switch to a runtime font CDN.
- Wide screens: `AmbientFrame` (in `shared/widgets`) centres the app in a 460 px column; test layout changes at about 380 px and at 1280 px, in light and dark.
