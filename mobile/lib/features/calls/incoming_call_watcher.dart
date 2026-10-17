import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/network/api_client.dart';
import 'calls_repository.dart';

/// Rings the app when someone calls. It polls the server every few seconds
/// while the person is signed in and the app is open (there is no push
/// notification channel yet, so a call cannot wake a closed app). Stops
/// polling for good if the server has calls turned off.
class IncomingCallWatcher extends ConsumerStatefulWidget {
  const IncomingCallWatcher({super.key, required this.child, this.interval = const Duration(seconds: 3)});
  final Widget child;
  final Duration interval;

  @override
  ConsumerState<IncomingCallWatcher> createState() => _IncomingCallWatcherState();
}

class _IncomingCallWatcherState extends ConsumerState<IncomingCallWatcher> {
  Timer? _timer;
  String? _shown;
  bool _checking = false;
  late final CallsRepository _repo;

  @override
  void initState() {
    super.initState();
    _repo = ref.read(callsRepositoryProvider);
    _timer = Timer.periodic(widget.interval, (_) => _check());
  }

  Future<void> _check() async {
    if (_checking || !mounted) return;
    _checking = true;
    try {
      final c = await _repo.incoming();
      if (!mounted) return;
      if (c != null && c.id != _shown) {
        _shown = c.id;
        context.push('/call/incoming', extra: c);
      }
    } on ApiException catch (e) {
      if (e.status == 404) _timer?.cancel(); // calls are not enabled on this server
    } catch (_) {
      // transient; the next tick retries
    } finally {
      _checking = false;
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
