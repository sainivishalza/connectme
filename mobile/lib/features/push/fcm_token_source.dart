import 'dart:async';

import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/foundation.dart';

import 'push_source.dart';

/// Push over Firebase Cloud Messaging (Android and iOS). The server sends
/// notification messages, so the phone shows them itself while the app is
/// closed; this class only registers the device, asks permission and reports
/// taps.
class FcmTokenSource implements PushTokenSource {
  FcmTokenSource(this._fm);
  final FirebaseMessaging _fm;

  @override
  String get platform => defaultTargetPlatform == TargetPlatform.iOS ? 'ios' : 'android';

  @override
  bool get available => true;

  @override
  Future<bool> isAuthorized() async {
    final s = (await _fm.getNotificationSettings()).authorizationStatus;
    return s == AuthorizationStatus.authorized || s == AuthorizationStatus.provisional;
  }

  @override
  Future<bool> requestPermission() async {
    final s = (await _fm.requestPermission()).authorizationStatus;
    return s == AuthorizationStatus.authorized || s == AuthorizationStatus.provisional;
  }

  // getToken and onTokenRefresh are marked deprecated in favour of register(),
  // but register() returns an installation id, not the registration token that
  // the server's FCM v1 sends use. Keep these until the server targets installation ids.
  @override
  // ignore: deprecated_member_use
  Future<String?> token() => _fm.getToken();

  @override
  // ignore: deprecated_member_use
  Stream<String> get onTokenRefresh => _fm.onTokenRefresh;

  PushTap? _tap(RemoteMessage m) {
    final kind = m.data['kind'], ref = m.data['ref'];
    if (kind is! String || kind.isEmpty) return null;
    return PushTap(kind: kind, ref: ref is String ? ref : '');
  }

  @override
  Stream<PushTap> get onOpened async* {
    // The notification that launched the app, then every later tap.
    final first = await _fm.getInitialMessage();
    final t = first == null ? null : _tap(first);
    if (t != null) yield t;
    await for (final m in FirebaseMessaging.onMessageOpenedApp) {
      final t = _tap(m);
      if (t != null) yield t;
    }
  }
}

/// Starts Firebase on Android and iOS builds that have a Firebase config file.
/// On the web, desktop, or any build where Firebase cannot start, the app
/// simply has no push channel instead of failing.
Future<PushTokenSource> initPush() async {
  if (kIsWeb || (defaultTargetPlatform != TargetPlatform.android && defaultTargetPlatform != TargetPlatform.iOS)) {
    return const NoPushTokenSource();
  }
  try {
    await Firebase.initializeApp();
    return FcmTokenSource(FirebaseMessaging.instance);
  } catch (_) {
    return const NoPushTokenSource();
  }
}
