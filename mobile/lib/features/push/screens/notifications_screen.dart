import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/states.dart';
import '../../digest/digest_repository.dart';
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
      try {
        _digest = await ref.read(digestRepositoryProvider).enabled();
      } catch (_) {
        // the summary switch is optional; the rest of the screen still works
      }
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

  Future<void> _pickTime(int current, QuietHours Function(int minutes) apply) async {
    final t = await showTimePicker(context: context, initialTime: TimeOfDay(hour: current ~/ 60, minute: current % 60));
    if (t == null || !mounted) return;
    await _set(_prefs.copyWith(quiet: apply(t.hour * 60 + t.minute)));
  }

  bool _digest = false;

  Future<void> _setDigest(bool on) async {
    final old = _digest;
    setState(() => _digest = on);
    if (!await guarded(context, () => ref.read(digestRepositoryProvider).set(on)) && mounted) setState(() => _digest = old);
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
                FilledButton(key: const Key('enable_push'), onPressed: _enable, child: Text(AppLocalizations.of(context)?.sTurnOnNotifications ?? 'Turn on notifications')),
              ]),
            ),
          ),
        const SizedBox(height: 8),
        SwitchListTile(key: const Key('pref_messages'), title: Text(AppLocalizations.of(context)?.sMessages ?? 'Messages'), subtitle: const Text('"You have a new message"'), value: _prefs.messages, onChanged: (v) => _set(_prefs.copyWith(messages: v))),
        SwitchListTile(key: const Key('pref_matches'), title: Text(AppLocalizations.of(context)?.sMatches ?? 'Matches'), subtitle: const Text('"You have a new match"'), value: _prefs.matches, onChanged: (v) => _set(_prefs.copyWith(matches: v))),
        SwitchListTile(key: const Key('pref_calls'), title: Text(AppLocalizations.of(context)?.sCalls ?? 'Calls'), subtitle: const Text('"Incoming call"'), value: _prefs.calls, onChanged: (v) => _set(_prefs.copyWith(calls: v))),
        SwitchListTile(key: const Key('pref_live'), title: const Text('People you follow go live'), subtitle: const Text('"Someone you follow is live"'), value: _prefs.live, onChanged: (v) => _set(_prefs.copyWith(live: v))),
        SwitchListTile(key: const Key('pref_comments'), title: Text(AppLocalizations.of(context)?.sRepliesToYourPosts ?? 'Replies to your posts'), subtitle: const Text('"Someone replied to your post"'), value: _prefs.comments, onChanged: (v) => _set(_prefs.copyWith(comments: v))),
        SwitchListTile(key: const Key('pref_bookings'), title: Text(AppLocalizations.of(context)?.sBookings ?? 'Bookings'), subtitle: const Text('"Your booking was updated"'), value: _prefs.bookings, onChanged: (v) => _set(_prefs.copyWith(bookings: v))),
        const Divider(),
        SwitchListTile(
          key: const Key('pref_digest'),
          title: const Text('Weekly summary email'),
          subtitle: const Text('Once a week: how many people liked, matched, followed or replied. Numbers only, no names'),
          value: _digest,
          onChanged: _setDigest,
        ),
        SwitchListTile(
          key: const Key('pref_quiet'),
          title: Text(AppLocalizations.of(context)?.sQuietHours ?? 'Quiet hours'),
          subtitle: Text(AppLocalizations.of(context)?.sNoNotificationsInThisWindow ?? 'No notifications in this window, except incoming calls'),
          value: _prefs.quiet.on,
          onChanged: (v) => _set(_prefs.copyWith(quiet: _prefs.quiet.copyWith(on: v))),
        ),
        if (_prefs.quiet.on) ...[
          ListTile(
            key: const Key('quiet_start'),
            title: Text(AppLocalizations.of(context)?.sFrom ?? 'From'),
            trailing: Text(QuietHours.label(_prefs.quiet.start)),
            onTap: () => _pickTime(_prefs.quiet.start, (m) => _prefs.quiet.copyWith(start: m)),
          ),
          ListTile(
            key: const Key('quiet_end'),
            title: Text(AppLocalizations.of(context)?.sUntil ?? 'Until'),
            trailing: Text(QuietHours.label(_prefs.quiet.end)),
            onTap: () => _pickTime(_prefs.quiet.end, (m) => _prefs.quiet.copyWith(end: m)),
          ),
        ],
        const SizedBox(height: 12),
        Text('Notifications never include names, photos or message text.', style: t.textTheme.bodySmall),
      ]);
    }
    return Scaffold(appBar: AppBar(title: Text(AppLocalizations.of(context)?.profileNotifications ?? 'Notifications')), body: body);
  }
}
