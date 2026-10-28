import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../features/chat/chat_providers.dart';
import '../../features/chat/data/realtime_client.dart';

/// Keeps a screen's state fresh. It polls every [interval] until the realtime
/// relay accepts the watch; from then on it refetches when the relay says the
/// topic changed and polls only as a slow safety net. If the relay refuses or
/// is unreachable the fast polling simply carries on, so a screen is never
/// stuck on old data.
class LiveRefresh {
  LiveRefresh({
    required RealtimeClient? client,
    required this.kind,
    required this.id,
    required this.interval,
    required this.onTick,
    this.slowFactor = 10,
  }) : _rt = client;

  factory LiveRefresh.of(
    WidgetRef ref, {
    required String kind,
    required String id,
    required Duration interval,
    required FutureOr<void> Function() onTick,
  }) => LiveRefresh(
    client: ref.read(realtimeProvider),
    kind: kind,
    id: id,
    interval: interval,
    onTick: onTick,
  );

  final RealtimeClient? _rt;
  final String kind;
  final String id;
  final Duration interval;
  final FutureOr<void> Function() onTick;
  final int slowFactor;

  Timer? _timer;
  Timer? _debounce;
  StreamSubscription<String>? _sub;
  bool _disposed = false;

  LiveRefresh start() {
    _setTimer(interval);
    final rt = _rt;
    if (rt == null) return this;
    _sub = rt.signals.listen((t) {
      if (t != '$kind:$id') return;
      _debounce?.cancel();
      _debounce = Timer(const Duration(milliseconds: 120), () {
        if (!_disposed) onTick();
      });
    });
    rt.watch(kind, id).then((ok) {
      if (ok && !_disposed) _setTimer(interval * slowFactor);
    }).catchError((_) {});
    return this;
  }

  void _setTimer(Duration d) {
    _timer?.cancel();
    _timer = Timer.periodic(d, (_) {
      if (!_disposed) onTick();
    });
  }

  void dispose() {
    _disposed = true;
    _timer?.cancel();
    _debounce?.cancel();
    _sub?.cancel();
    _rt?.unwatch(kind, id);
  }
}
