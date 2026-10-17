import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

/// Where a tapped notification should take the person.
class PushTap {
  const PushTap({required this.kind, required this.ref});
  final String kind; // message | match | call | booking
  final String ref; // an opaque id (a match or call id), never a name
}

/// The only place the app touches the phone's push system (Firebase Cloud
/// Messaging on Android and iOS). It is an interface so screens and tests
/// never depend on a platform plugin, and so the app builds and runs without
/// a Firebase project.
///
/// To turn push on for a build: add `firebase_core` and `firebase_messaging`,
/// the platform's Firebase config files, write a class implementing this
/// interface over `FirebaseMessaging`, and override [pushTokenSourceProvider]
/// with it in `main.dart` (see docs/RUNBOOK.md, "Push notifications").
abstract class PushTokenSource {
  /// 'android', 'ios' or 'web' (what the server stores).
  String get platform;

  /// False when this build has no push channel at all.
  bool get available;

  /// Whether the person already allowed notifications (never prompts).
  Future<bool> isAuthorized();

  /// Shows the system permission prompt; true when allowed.
  Future<bool> requestPermission();

  /// The delivery token for this install, or null when there is none.
  Future<String?> token();

  /// Emits a new token when the provider rotates it.
  Stream<String> get onTokenRefresh;

  /// Emits when the person taps a notification (including the one that
  /// launched the app).
  Stream<PushTap> get onOpened;
}

/// The default: no push channel. Everything push-related hides or explains
/// itself instead of failing.
class NoPushTokenSource implements PushTokenSource {
  const NoPushTokenSource();
  @override
  String get platform => 'web';
  @override
  bool get available => false;
  @override
  Future<bool> isAuthorized() async => false;
  @override
  Future<bool> requestPermission() async => false;
  @override
  Future<String?> token() async => null;
  @override
  Stream<String> get onTokenRefresh => const Stream.empty();
  @override
  Stream<PushTap> get onOpened => const Stream.empty();
}

final pushTokenSourceProvider = Provider<PushTokenSource>((ref) => const NoPushTokenSource());
