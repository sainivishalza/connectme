import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'push_repository.dart';
import 'push_source.dart';

/// Keeps this install's push token registered with the server while someone
/// is signed in, and opens the right screen when a notification is tapped.
/// It never shows a permission prompt: that happens only when the person turns
/// notifications on in Settings. If they already allowed them, registration is
/// silent.
class PushRegistrar extends ConsumerStatefulWidget {
  const PushRegistrar({super.key, required this.child});
  final Widget child;

  @override
  ConsumerState<PushRegistrar> createState() => _PushRegistrarState();
}

class _PushRegistrarState extends ConsumerState<PushRegistrar> {
  final List<StreamSubscription<dynamic>> _subs = [];

  @override
  void initState() {
    super.initState();
    final src = ref.read(pushTokenSourceProvider);
    if (!src.available) return;
    _register();
    _subs.add(src.onTokenRefresh.listen(_send));
    _subs.add(src.onOpened.listen(_open));
  }

  Future<void> _register() async {
    try {
      final src = ref.read(pushTokenSourceProvider);
      if (!await src.isAuthorized()) return;
      final t = await src.token();
      if (t != null) await _send(t);
    } catch (_) {
      // push is best effort; the app works without it
    }
  }

  Future<void> _send(String token) async {
    try {
      await ref.read(pushRepositoryProvider).register(token, ref.read(pushTokenSourceProvider).platform);
    } catch (_) {}
  }

  void _open(PushTap tap) {
    if (!mounted) return;
    final router = GoRouter.of(context);
    switch (tap.kind) {
      case 'message' || 'match':
        router.push('/chats/${tap.ref}');
      case 'report':
        router.push('/safety/reports');
      // A call rings through the incoming-call watcher as soon as the app is open.
    }
  }

  @override
  void dispose() {
    for (final s in _subs) {
      s.cancel();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
