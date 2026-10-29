# Android release signing

The release build is no longer signed with the debug key. Without a key it is left **unsigned**, so it cannot be installed or uploaded by mistake. Debug builds (`flutter build apk --debug`) are unchanged.

## One-time setup (the owner does this, on their own computer)

1. Create a key. Pick strong passwords and keep them in a password manager. Losing this key means you can never update the app on Google Play under the same listing.

   ```bash
   keytool -genkey -v -keystore ~/connectme-release.jks -keyalg RSA -keysize 2048 -validity 10000 -alias connectme
   ```

2. Back the `.jks` file up somewhere safe and private (not this repository, not a public cloud folder).
3. Create `mobile/android/key.properties` (already gitignored, with `*.jks`):

   ```properties
   storeFile=C:/Users/you/connectme-release.jks
   storePassword=<store password>
   keyAlias=connectme
   keyPassword=<key password>
   ```

4. Build: `flutter build appbundle --release` (for Google Play) or `flutter build apk --release`.

If `key.properties` is missing or incomplete, Gradle prints a warning and the release output stays unsigned.

## Backups are off

`android:allowBackup="false"` and `res/xml/data_extraction_rules.xml` stop cloud backup, `adb backup` and device-to-device transfer from copying chat keys, cached messages or hotel-mode data off the phone.
