import 'package:flutter/material.dart';
import '../../../core/theme/tokens.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/models.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/avatar.dart';
import '../../../shared/widgets/states.dart';
import '../../auth/auth_controller.dart';
import '../../payments/payments_repository.dart';
import '../profile_repository.dart';
import '../widgets/mode_switch.dart';

class ProfileScreen extends ConsumerStatefulWidget {
  const ProfileScreen({super.key});
  @override
  ConsumerState<ProfileScreen> createState() => _ProfileScreenState();
}

class _ProfileScreenState extends ConsumerState<ProfileScreen> {
  bool _busy = false;

  Future<void> _setMode(String mode) async {
    setState(() => _busy = true);
    await guarded(context, () async {
      await ref.read(profileRepositoryProvider).setMode(mode);
      ref.invalidate(meProvider);
    });
    if (mounted) setState(() => _busy = false);
  }

  Future<void> _setInvisible(bool on) async {
    await guarded(context, () async {
      await ref.read(profileRepositoryProvider).setInvisible(on);
      ref.invalidate(meProvider);
    });
  }

  Future<void> _export(BuildContext context) async {
    String? text;
    await guarded(context, () async => text = await ref.read(profileRepositoryProvider).exportData());
    if (text == null || !context.mounted) return;
    await showDialog<void>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('Your data'),
        content: SizedBox(width: double.maxFinite, child: SingleChildScrollView(child: SelectableText(text!))),
        actions: [
          TextButton(
            onPressed: () async {
              await Clipboard.setData(ClipboardData(text: text!));
              if (c.mounted) Navigator.pop(c);
            },
            child: const Text('Copy'),
          ),
          TextButton(onPressed: () => Navigator.pop(c), child: const Text('Close')),
        ],
      ),
    );
  }

  Future<void> _changePassword(BuildContext context) async {
    final cur = TextEditingController();
    final next = TextEditingController();
    final go = await showDialog<bool>(
      context: context,
      builder: (c) => StatefulBuilder(
        builder: (c, set) => AlertDialog(
          title: const Text('Change password'),
          content: Column(mainAxisSize: MainAxisSize.min, children: [
            const Text('Other devices will be signed out.'),
            TextField(
                key: const Key('current_password'),
                controller: cur,
                obscureText: true,
                decoration: const InputDecoration(labelText: 'Current password'),
                onChanged: (_) => set(() {})),
            TextField(
                key: const Key('new_password'),
                controller: next,
                obscureText: true,
                decoration: const InputDecoration(labelText: 'New password (12+ characters)'),
                onChanged: (_) => set(() {})),
          ]),
          actions: [
            TextButton(onPressed: () => Navigator.pop(c, false), child: const Text('Cancel')),
            TextButton(
              key: const Key('change_password_go'),
              onPressed: cur.text.isNotEmpty && next.text.length >= 12 ? () => Navigator.pop(c, true) : null,
              child: const Text('Change'),
            ),
          ],
        ),
      ),
    );
    final current = cur.text, updated = next.text;
    // Dispose after the dialog has finished animating closed, not while its text fields still listen.
    Future<void>.delayed(const Duration(milliseconds: 500), () {
      cur.dispose();
      next.dispose();
    });
    if (go != true || !context.mounted) return;
    final ok = await guarded(context, () => ref.read(authControllerProvider.notifier).changePassword(current, updated));
    if (ok && context.mounted) showMessage(context, 'Password changed. Other devices were signed out.');
  }

  Future<void> _erase(BuildContext context) async {
    final pw = TextEditingController();
    final word = TextEditingController();
    final go = await showDialog<bool>(
      context: context,
      builder: (c) => StatefulBuilder(
        builder: (c, set) => AlertDialog(
          title: const Text('Delete your account?'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Text('This cannot be undone. Your profile, photos, matches and chats are permanently deleted and '
                  'any subscription is cancelled. Chat history on this device is erased too.'),
              const SizedBox(height: 12),
              TextField(
                  key: const Key('erase_password'),
                  controller: pw,
                  obscureText: true,
                  decoration: const InputDecoration(labelText: 'Password'),
                  onChanged: (_) => set(() {})),
              TextField(
                  key: const Key('erase_confirm'),
                  controller: word,
                  decoration: const InputDecoration(labelText: 'Type DELETE to confirm'),
                  onChanged: (_) => set(() {})),
            ],
          ),
          actions: [
            TextButton(onPressed: () => Navigator.pop(c, false), child: const Text('Cancel')),
            TextButton(
              key: const Key('erase_go'),
              onPressed: pw.text.isNotEmpty && word.text == 'DELETE' ? () => Navigator.pop(c, true) : null,
              child: Text('Delete forever', style: TextStyle(color: Theme.of(c).colorScheme.error)),
            ),
          ],
        ),
      ),
    );
    final password = pw.text;
    Future<void>.delayed(const Duration(milliseconds: 500), () {
      pw.dispose();
      word.dispose();
    });
    if (go != true || !context.mounted) return;
    // On success the router sends the user to the login screen.
    await guarded(context, () => ref.read(authControllerProvider.notifier).eraseAccount(password));
  }

  @override
  Widget build(BuildContext context) {
    final me = ref.watch(meProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Profile'), actions: [
        IconButton(
          tooltip: 'Edit',
          icon: const Icon(Icons.edit_outlined),
          onPressed: () => context.push('/profile/edit'),
        ),
      ]),
      body: me.when(
        loading: () => const Loader(),
        error: (e, _) => ErrorView(error: e, onRetry: () => ref.invalidate(meProvider)),
        data: (p) => RefreshIndicator(
          onRefresh: () async => ref.refresh(meProvider.future),
          child: ListView(
            padding: const EdgeInsets.all(16),
            children: [
              _Header(p: p),
              const SizedBox(height: 16),
              Text('Who can see you', style: Theme.of(context).textTheme.titleMedium),
              const SizedBox(height: 8),
              ModeSwitch(mode: p.mode, busy: _busy, onChanged: _setMode),
              const SizedBox(height: 8),
              SwitchListTile(
                key: const Key('invisible'),
                contentPadding: EdgeInsets.zero,
                title: const Text('Invisible mode'),
                subtitle: const Text('Hide from nearby and hotel discovery (Gold)'),
                value: p.invisible,
                onChanged: _setInvisible,
              ),
              const Divider(),
              _AccountTiles(),
              const Divider(),
              ListTile(
                key: const Key('export_data'),
                leading: const Icon(Icons.download_outlined),
                title: const Text('Download my data'),
                subtitle: const Text('Everything we hold about you'),
                onTap: () => _export(context),
              ),
              ListTile(
                key: const Key('notifications'),
                leading: const Icon(Icons.notifications_outlined),
                title: const Text('Notifications'),
                subtitle: const Text('Messages, matches, calls'),
                onTap: () => context.push('/settings/notifications'),
              ),
              ListTile(
                key: const Key('change_password'),
                leading: const Icon(Icons.password),
                title: const Text('Change password'),
                onTap: () => _changePassword(context),
              ),
              ListTile(
                leading: const Icon(Icons.logout),
                title: const Text('Sign out'),
                onTap: () => ref.read(authControllerProvider.notifier).logout(),
              ),
              ListTile(
                key: const Key('logout_everywhere'),
                leading: const Icon(Icons.devices_other),
                title: const Text('Sign out of all devices'),
                onTap: () => guarded(context, () => ref.read(authControllerProvider.notifier).logoutEverywhere()),
              ),
              ListTile(
                key: const Key('delete_account'),
                leading: Icon(Icons.delete_forever_outlined, color: Theme.of(context).colorScheme.error),
                title: Text('Delete my account', style: TextStyle(color: Theme.of(context).colorScheme.error)),
                subtitle: const Text('Permanent. Removes your profile, matches and chats.'),
                onTap: () => _erase(context),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _Header extends StatelessWidget {
  const _Header({required this.p});
  final OwnProfile p;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Row(
      children: [
        Avatar(name: p.displayName, photoPath: p.photos.isEmpty ? null : p.photos.first.path, radius: 40, mode: p.mode),
        const SizedBox(width: 18),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(p.displayName.isEmpty ? 'Add your name' : p.displayName, style: t.textTheme.headlineMedium),
              const SizedBox(height: 8),
              Wrap(spacing: 8, runSpacing: 6, children: [
                Chip(label: Text('${p.age} years'), visualDensity: VisualDensity.compact),
                Chip(label: Text(ModeLight.of(p.mode).label), visualDensity: VisualDensity.compact),
              ]),
              if (!p.emailVerified)
                Padding(
                  padding: const EdgeInsets.only(top: 8),
                  child: Text('Email not verified', style: TextStyle(color: t.colorScheme.error)),
                ),
            ],
          ),
        ),
      ],
    );
  }
}

class _AccountTiles extends ConsumerWidget {
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final acct = ref.watch(accountProvider);
    final coins = acct.maybeWhen(data: (a) => '${a.coins} coins', orElse: () => '');
    final tier = acct.maybeWhen(data: (a) => a.tier.isEmpty ? 'Free plan' : '${a.tier[0].toUpperCase()}${a.tier.substring(1)} plan', orElse: () => '');
    return Column(
      children: [
        ListTile(
          leading: const Icon(Icons.workspace_premium_outlined),
          title: const Text('Subscription'),
          subtitle: Text(tier),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => context.push('/profile/premium'),
        ),
        ListTile(
          leading: const Icon(Icons.monetization_on_outlined),
          title: const Text('Coins'),
          subtitle: Text(coins),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => context.push('/profile/coins'),
        ),
        ListTile(
          leading: const Icon(Icons.card_giftcard),
          title: const Text('Gifts received'),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => context.push('/profile/gifts'),
        ),
      ],
    );
  }
}
