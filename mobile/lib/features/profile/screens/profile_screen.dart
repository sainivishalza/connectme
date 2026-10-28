import 'package:flutter/material.dart';
import '../../../core/theme/tokens.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/models.dart';
import '../../../l10n/app_localizations.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/avatar.dart';
import '../../../shared/widgets/states.dart';
import '../../auth/auth_controller.dart';
import '../../payments/payments_repository.dart';
import '../profile_repository.dart';
import '../widgets/completeness_card.dart';
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
        title: Text(AppLocalizations.of(context)?.sYourData ?? 'Your data'),
        content: SizedBox(width: double.maxFinite, child: SingleChildScrollView(child: SelectableText(text!))),
        actions: [
          TextButton(
            onPressed: () async {
              await Clipboard.setData(ClipboardData(text: text!));
              if (c.mounted) Navigator.pop(c);
            },
            child: Text(AppLocalizations.of(context)?.sCopy ?? 'Copy'),
          ),
          TextButton(onPressed: () => Navigator.pop(c), child: Text(AppLocalizations.of(context)?.sClose ?? 'Close')),
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
          title: Text(AppLocalizations.of(context)?.sChangePassword ?? 'Change password'),
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
            TextButton(onPressed: () => Navigator.pop(c, false), child: Text(AppLocalizations.of(context)?.sCancel ?? 'Cancel')),
            TextButton(
              key: const Key('change_password_go'),
              onPressed: cur.text.isNotEmpty && next.text.length >= 12 ? () => Navigator.pop(c, true) : null,
              child: Text(AppLocalizations.of(context)?.sChange ?? 'Change'),
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
          title: Text(AppLocalizations.of(context)?.sDeleteYourAccount ?? 'Delete your account?'),
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
            TextButton(onPressed: () => Navigator.pop(c, false), child: Text(AppLocalizations.of(context)?.sCancel ?? 'Cancel')),
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
      appBar: AppBar(title: Text(AppLocalizations.of(context)?.navProfile ?? 'Profile'), actions: [
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
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
            children: [
              _Header(p: p),
              const SizedBox(height: 16),
              CompletenessCard(completeness: p.completeness),
              const SizedBox(height: 8),
              const _SectionTitle('Your profile'),
              _Group(children: [
                ListTile(
                  key: const Key('about_you'),
                  leading: const Icon(Icons.tune),
                  title: Text(AppLocalizations.of(context)?.profileAboutYou ?? 'About you'),
                  subtitle: Text(p.details.goal.isEmpty ? 'What you are looking for and how you live' : (DetailLabels.goal[p.details.goal] ?? '')),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => context.push('/profile/about'),
                ),
                ListTile(
                  key: const Key('your_prompts'),
                  leading: const Icon(Icons.chat_bubble_outline),
                  title: Text(AppLocalizations.of(context)?.profilePrompts ?? 'Your prompts'),
                  subtitle: Text(p.prompts.isEmpty ? 'Give people something to talk about' : '${p.prompts.length} answered'),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => context.push('/profile/prompts'),
                ),
                ListTile(
                  key: const Key('verify_id'),
                  leading: Icon(p.idVerified ? Icons.verified : Icons.badge_outlined),
                  title: Text(p.idVerified ? 'ID verified' : 'Verify with ID'),
                  subtitle: Text(p.idVerified ? 'Your ID has been checked' : 'Send a photo ID and a selfie'),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => context.push('/profile/verify-id'),
                ),
                ListTile(
                  key: const Key('quiz'),
                  leading: const Icon(Icons.psychology_alt_outlined),
                  title: Text(AppLocalizations.of(context)?.profileQuiz ?? 'Compatibility quiz'),
                  subtitle: const Text('See how well you match before you see a photo'),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => context.push('/profile/quiz'),
                ),
              ]),
              const _SectionTitle('Who can see you'),
              ModeSwitch(mode: p.mode, busy: _busy, onChanged: _setMode),
              const SizedBox(height: 12),
              _Group(children: [
                SwitchListTile(
                  key: const Key('invisible'),
                  title: Text(AppLocalizations.of(context)?.sInvisibleMode ?? 'Invisible mode'),
                  subtitle: const Text('Hide from nearby and hotel discovery (Gold)'),
                  value: p.invisible,
                  onChanged: _setInvisible,
                ),
              ]),
              const _SectionTitle('Account'),
              _Group(children: [
                _AccountTiles(),
                ListTile(
                  key: const Key('levels'),
                  leading: const Icon(Icons.auto_awesome_outlined),
                  title: Text(AppLocalizations.of(context)?.profileLevels ?? 'Levels and leaderboards'),
                  subtitle: const Text('Charm, Supporter and who is on top'),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => context.push('/profile/levels'),
                ),
                ListTile(
                  key: const Key('following'),
                  leading: const Icon(Icons.people_outline),
                  title: Text(AppLocalizations.of(context)?.profileFollowing ?? 'Following'),
                  subtitle: const Text('People you follow and your followers'),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => context.push('/profile/following'),
                ),
              ]),
              const _SectionTitle('Safety and settings'),
              _Group(children: [
                ListTile(
                  key: const Key('safety'),
                  leading: const Icon(Icons.shield_outlined),
                  title: Text(AppLocalizations.of(context)?.profileSafety ?? 'Safety center'),
                  subtitle: const Text('Date plans, people you trust, tips'),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => context.push('/safety'),
                ),
                ListTile(
                  key: const Key('notifications'),
                  leading: const Icon(Icons.notifications_outlined),
                  title: Text(AppLocalizations.of(context)?.profileNotifications ?? 'Notifications'),
                  subtitle: const Text('Messages, matches, calls'),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => context.push('/settings/notifications'),
                ),
                ListTile(
                  key: const Key('appearance'),
                  leading: const Icon(Icons.palette_outlined),
                  title: const Text('Appearance'),
                  subtitle: const Text('Dark mode, text size, reduce motion'),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => context.push('/settings/appearance'),
                ),
                ListTile(
                  key: const Key('change_password'),
                  leading: const Icon(Icons.password),
                  title: Text(AppLocalizations.of(context)?.sChangePassword ?? 'Change password'),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => _changePassword(context),
                ),
                ListTile(
                  key: const Key('export_data'),
                  leading: const Icon(Icons.download_outlined),
                  title: Text(AppLocalizations.of(context)?.sDownloadMyData ?? 'Download my data'),
                  subtitle: const Text('Everything we hold about you'),
                  onTap: () => _export(context),
                ),
              ]),
              const _SectionTitle('About ConnectMe'),
              _Group(children: [
                ListTile(
                  key: const Key('invest'),
                  leading: const Icon(Icons.trending_up),
                  title: Text(AppLocalizations.of(context)?.profileInvest ?? 'Invest in ConnectMe'),
                  subtitle: const Text('See who owns the app and pledge to become a partner'),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => context.push('/invest'),
                ),
              ]),
              const SizedBox(height: 20),
              _Group(children: [
                ListTile(
                  leading: const Icon(Icons.logout),
                  title: Text(AppLocalizations.of(context)?.profileSignOut ?? 'Sign out'),
                  onTap: () => ref.read(authControllerProvider.notifier).logout(),
                ),
                ListTile(
                  key: const Key('logout_everywhere'),
                  leading: const Icon(Icons.devices_other),
                  title: Text(AppLocalizations.of(context)?.sSignOutOfAllDevices ?? 'Sign out of all devices'),
                  onTap: () => guarded(context, () => ref.read(authControllerProvider.notifier).logoutEverywhere()),
                ),
                ListTile(
                  key: const Key('delete_account'),
                  leading: Icon(Icons.delete_forever_outlined, color: Theme.of(context).colorScheme.error),
                  title: Text('Delete my account', style: TextStyle(color: Theme.of(context).colorScheme.error)),
                  subtitle: const Text('Permanent. Removes your profile, matches and chats.'),
                  onTap: () => _erase(context),
                ),
              ]),
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
          title: Text(AppLocalizations.of(context)?.sSubscription ?? 'Subscription'),
          subtitle: Text(tier),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => context.push('/profile/premium'),
        ),
        _hairline(context),
        ListTile(
          leading: const Icon(Icons.monetization_on_outlined),
          title: Text(AppLocalizations.of(context)?.sCoins ?? 'Coins'),
          subtitle: Text(coins),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => context.push('/profile/coins'),
        ),
        _hairline(context),
        ListTile(
          leading: const Icon(Icons.card_giftcard),
          title: Text(AppLocalizations.of(context)?.sGiftsReceived ?? 'Gifts received'),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => context.push('/profile/gifts'),
        ),
      ],
    );
  }
}

/// A heading above a group of rows.
class _SectionTitle extends StatelessWidget {
  const _SectionTitle(this.text);
  final String text;

  @override
  Widget build(BuildContext context) => Padding(
        padding: const EdgeInsets.fromLTRB(4, 22, 4, 10),
        child: Text(text, style: Theme.of(context).textTheme.titleMedium),
      );
}

Widget _hairline(BuildContext context) =>
    Divider(height: 1, indent: 60, color: Theme.of(context).colorScheme.outlineVariant.withValues(alpha: 0.5));

/// Rows that belong together on one soft card, with a hairline between them,
/// so the page reads as a few groups instead of one long list.
class _Group extends StatelessWidget {
  const _Group({required this.children});
  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: Theme.of(context).colorScheme.surfaceContainerLow,
      borderRadius: BorderRadius.circular(Radii.card),
      clipBehavior: Clip.antiAlias,
      child: Column(children: [
        for (var i = 0; i < children.length; i++) ...[
          if (i > 0) _hairline(context),
          children[i],
        ],
      ]),
    );
  }
}
