import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../push_repository.dart';
import '../push_source.dart';

/// Choose which notifications reach your phone. They are deliberately plain
/// ("You have a new message"): no names, no message text, nothing from an
/// anonymous chat appears on a lock screen.
class NotificationsScreen extends ConsumerStatefulWidget {
  const NotificationsScreen({super.key});

  @override
  ConsumerState<NotificationsScreen> createState() => _NotificationsScreenState();
}

class _NotificationsScreenState extends ConsumerState<NotificationsScreen> {
  PushPrefs _prefs = const PushPrefs();
  int _devices = 0;
  bool _allowed = false;
  Object? _error;
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final r = await ref.read(pushRepositoryProvider).prefs();
      final allowed = await ref.read(pushTokenSourceProvider).isAuthorized();
      if (mounted) {
        setState(() {
          _prefs = r.prefs;
          _devices = r.devices;
          _allowed = allowed;
          _error = null;
          _loading = false;
        });
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = e;
          _loading = false;
        });
      }
    }
  }

  Future<void> _enable() async {
    final src = ref.read(pushTokenSourceProvider);
    final ok = await src.requestPermission();
    if (!ok) {
      if (mounted) showMessage(context, 'Notifications are blocked. You can allow them in your phone settings.');
      return;
    }
    final token = await src.token();
    if (token == null) {
      if (mounted) showMessage(context, 'This phone could not get a notification address. Try again later.');
      return;
    }
    if (!mounted) return;
    if (await guarded(context, () => ref.read(pushRepositoryProvider).register(token, src.platform))) {
      _load();
    }
  }

  Future<void> _set(PushPrefs next) async {
    final old = _prefs;
    setState(() => _prefs = next);
    if (!await guarded(context, () => ref.read(pushRepositoryProvider).setPrefs(next)) && mounted) setState(() => _prefs = old);
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final src = ref.watch(pushTokenSourceProvider);
    Widget body;
    if (_loading) {
      body = const Loader();
    } else if (_error != null) {
      body = ErrorView(error: _error!, onRetry: () {
        setState(() => _loading = true);
        _load();
      });
    } else {
      body = ListView(padding: const EdgeInsets.fromLTRB(16, 4, 16, 32), children: [
        if (!src.available)
          Card(
            key: const Key('push_unavailable'),
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Text('Notifications are not available in this version of the app. Your choices below are saved and apply as soon as they are.', style: t.textTheme.bodyMedium),
            ),
          )
        else if (!_allowed || _devices == 0)
          Card(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text('Notifications are off on this phone', style: t.textTheme.titleMedium),
                const SizedBox(height: 6),
                Text('Turn them on to hear about new messages, matches and calls when the app is closed.', style: t.textTheme.bodyMedium),
                const SizedBox(height: 12),
                FilledButton(key: const Key('enable_push'), onPressed: _enable, child: const Text('Turn on notifications')),
              ]),
            ),
          ),
        const SizedBox(height: 8),
        SwitchListTile(key: const Key('pref_messages'), title: const Text('Messages'), subtitle: const Text('"You have a new message"'), value: _prefs.messages, onChanged: (v) => _set(_prefs.copyWith(messages: v))),
        SwitchListTile(key: const Key('pref_matches'), title: const Text('Matches'), subtitle: const Text('"You have a new match"'), value: _prefs.matches, onChanged: (v) => _set(_prefs.copyWith(matches: v))),
        SwitchListTile(key: const Key('pref_calls'), title: const Text('Calls'), subtitle: const Text('"Incoming call"'), value: _prefs.calls, onChanged: (v) => _set(_prefs.copyWith(calls: v))),
        SwitchListTile(key: const Key('pref_bookings'), title: const Text('Bookings'), subtitle: const Text('"Your booking was updated"'), value: _prefs.bookings, onChanged: (v) => _set(_prefs.copyWith(bookings: v))),
        const SizedBox(height: 12),
        Text('Notifications never include names, photos or message text.', style: t.textTheme.bodySmall),
      ]);
    }
    return Scaffold(appBar: AppBar(title: const Text('Notifications')), body: body);
  }
}
