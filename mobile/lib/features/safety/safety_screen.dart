import 'package:flutter/material.dart';
import '../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../shared/widgets/app_snack.dart';
import '../../shared/widgets/states.dart';
import 'safety_repository.dart';

const safetyTips = [
  'Meet in a public place, and tell someone where you are going.',
  'Arrange your own way there and back.',
  'Never send money, gift cards or crypto to someone you have not met.',
  'Keep chatting here until you trust them: links and other apps are where scams continue.',
  'If something feels wrong, leave. You can block and report from the chat menu at any time.',
];

String clock(DateTime t) {
  final h = t.hour.toString().padLeft(2, '0'), m = t.minute.toString().padLeft(2, '0');
  final now = DateTime.now();
  final today = t.year == now.year && t.month == now.month && t.day == now.day;
  return today ? '$h:$m' : '${t.day}/${t.month} $h:$m';
}

/// Safety center: tips, people you trust, and date plans with a check-in.
/// Opened from a chat it pre-fills a plan for that match.
class SafetyScreen extends ConsumerStatefulWidget {
  const SafetyScreen({super.key, this.matchId});
  final String? matchId;

  @override
  ConsumerState<SafetyScreen> createState() => _SafetyScreenState();
}

class _SafetyScreenState extends ConsumerState<SafetyScreen> {
  Future<void> _addContact() async {
    final r = await showDialog<(String, String)>(context: context, builder: (_) => const _ContactDialog());
    if (r == null || !mounted) return;
    final ok = await guarded(context, () => ref.read(safetyRepositoryProvider).addContact(r.$1, r.$2));
    if (ok && mounted) {
      ref.invalidate(trustedContactsProvider);
      showMessage(context, 'We emailed ${r.$1} to ask if they agree.');
    }
  }

  Future<void> _newPlan(List<TrustedContact> contacts) async {
    final usable = contacts.where((c) => c.confirmed).toList();
    if (usable.isEmpty) {
      showMessage(context, 'Add a trusted contact and wait for them to confirm first.');
      return;
    }
    final ok = await showModalBottomSheet<bool>(
      context: context, useRootNavigator: true,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (_) => _PlanSheet(contacts: usable, matchId: widget.matchId),
    );
    if (ok == true) {
      ref.invalidate(datePlansProvider);
    }
  }

  Future<void> _do(Future<void> Function() f, String done) async {
    final ok = await guarded(context, f);
    if (ok && mounted) {
      ref.invalidate(datePlansProvider);
      showMessage(context, done);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final l = AppLocalizations.of(context);
    final tips = l == null ? safetyTips : [l.sfTip1, l.sfTip2, l.sfTip3, l.sfTip4, l.sfTip5];
    final contacts = ref.watch(trustedContactsProvider);
    final plans = ref.watch(datePlansProvider);
    final repo = ref.read(safetyRepositoryProvider);
    return Scaffold(
      appBar: AppBar(title: Text(AppLocalizations.of(context)?.profileSafety ?? 'Safety center')),
      body: ListView(padding: const EdgeInsets.all(16), children: [
        Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(l?.sfBeforeYouMeet ?? 'Before you meet', style: t.textTheme.titleMedium),
              const SizedBox(height: 8),
              for (final tip in tips)
                Padding(padding: const EdgeInsets.only(bottom: 6), child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  const Padding(padding: EdgeInsets.only(top: 3), child: Icon(Icons.check_circle_outline, size: 16)),
                  const SizedBox(width: 8),
                  Expanded(child: Text(tip, style: t.textTheme.bodyMedium)),
                ])),
            ]),
          ),
        ),
        const SizedBox(height: 20),
        Text(l?.sfDatePlans ?? 'Date plans', style: t.textTheme.titleLarge),
        const SizedBox(height: 4),
        Text(l?.sfDatePlansHint ?? 'Tell someone you trust where you will be. If you do not check in, they are emailed.', style: t.textTheme.bodySmall),
        const SizedBox(height: 8),
        plans.when(
          loading: () => const Padding(padding: EdgeInsets.all(16), child: Loader()),
          error: (e, _) => ErrorView(error: e, onRetry: () => ref.invalidate(datePlansProvider)),
          data: (list) => Column(children: [
            for (final p in list.where((p) => p.open)) _PlanCard(
              plan: p,
              onSafe: () => _do(() => repo.safe(p.id), 'Checked in. Your contact has been told.'),
              onExtend: () => _do(() => repo.extend(p.id, 30), '30 more minutes'),
              onCancel: () => _do(() => repo.cancel(p.id), 'Plan cancelled'),
            ),
            if (!list.any((p) => p.open)) Padding(padding: const EdgeInsets.symmetric(vertical: 8), child: Text(l?.sfNoPlans ?? 'No plans right now.')),
            const SizedBox(height: 8),
            FilledButton.icon(
              key: const Key('new_plan'),
              onPressed: () => _newPlan(contacts.asData?.value ?? const []),
              icon: const Icon(Icons.event_available_outlined),
              label: Text(AppLocalizations.of(context)?.sShareADatePlan ?? 'Share a date plan'),
            ),
          ]),
        ),
        const SizedBox(height: 24),
        Text(l?.sfPeopleYouTrust ?? 'People you trust', style: t.textTheme.titleLarge),
        const SizedBox(height: 4),
        Text(l?.sfPeopleHint ?? 'They have to agree first, and only ever get what you choose to send. Up to three.', style: t.textTheme.bodySmall),
        const SizedBox(height: 8),
        contacts.when(
          loading: () => const Padding(padding: EdgeInsets.all(16), child: Loader()),
          error: (e, _) => ErrorView(error: e, onRetry: () => ref.invalidate(trustedContactsProvider)),
          data: (list) => Column(children: [
            for (final c in list)
              Card(
                child: ListTile(
                  key: Key('contact_${c.id}'),
                  leading: Icon(c.confirmed ? Icons.verified_user_outlined : Icons.hourglass_empty),
                  title: Text(c.name),
                  subtitle: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                    Text('${c.email} - ${c.confirmed ? 'agreed' : 'waiting for them to agree'}'),
                    if (c.confirmed) _PhoneRow(contact: c, repo: repo),
                  ]),
                  trailing: IconButton(
                    key: Key('remove_${c.id}'),
                    tooltip: 'Remove ${c.name}',
                    icon: const Icon(Icons.delete_outline),
                    onPressed: () async {
                      final ok = await guarded(context, () => repo.removeContact(c.id));
                      if (ok) {
                        ref.invalidate(trustedContactsProvider);
                        ref.invalidate(datePlansProvider);
                      }
                    },
                  ),
                ),
              ),
            if (list.length < 3)
              OutlinedButton.icon(key: const Key('add_contact'), onPressed: _addContact, icon: const Icon(Icons.person_add_alt), label: Text(AppLocalizations.of(context)?.sAddSomeoneYouTrust ?? 'Add someone you trust')),
          ]),
        ),
        const SizedBox(height: 16),
        ListTile(
          key: const Key('my_reports'),
          leading: const Icon(Icons.flag_outlined),
          title: Text(l?.sfMyReports ?? 'My reports'),
          subtitle: Text(l?.sfMyReportsHint ?? 'See what happened to reports you sent'),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => context.push('/safety/reports'),
        ),
      ]),
    );
  }
}

class _PlanCard extends StatelessWidget {
  const _PlanCard({required this.plan, required this.onSafe, required this.onExtend, required this.onCancel});
  final DatePlan plan;
  final VoidCallback onSafe;
  final VoidCallback onExtend;
  final VoidCallback onCancel;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final alerted = plan.status == 'alerted';
    return Card(
      key: Key('plan_${plan.id}'),
      color: alerted ? t.colorScheme.errorContainer : null,
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Text(plan.place, style: t.textTheme.titleMedium),
          const SizedBox(height: 4),
          Text('${plan.who.isEmpty ? 'Meeting' : 'Meeting ${plan.who}'} at ${clock(plan.meetAt)}. Check in by ${clock(plan.checkinBy)}. ${plan.contact} knows.', style: t.textTheme.bodyMedium),
          if (alerted) Padding(padding: const EdgeInsets.only(top: 6), child: Text('You did not check in, so ${plan.contact} was emailed. Tap "I am safe" to let them know.', key: const Key('alerted_note'), style: t.textTheme.bodySmall)),
          const SizedBox(height: 8),
          Wrap(spacing: 8, runSpacing: 4, children: [
            FilledButton(key: Key('safe_${plan.id}'), onPressed: onSafe, child: Text(AppLocalizations.of(context)?.sIAmSafe ?? 'I am safe')),
            if (plan.canExtend) OutlinedButton(key: Key('extend_${plan.id}'), onPressed: onExtend, child: const Text('Need 30 more minutes')),
            TextButton(key: Key('cancel_plan_${plan.id}'), onPressed: onCancel, child: Text(AppLocalizations.of(context)?.sCancelPlan ?? 'Cancel plan')),
          ]),
        ]),
      ),
    );
  }
}

class _ContactDialog extends StatefulWidget {
  const _ContactDialog();

  @override
  State<_ContactDialog> createState() => _ContactDialogState();
}

class _ContactDialogState extends State<_ContactDialog> {
  final _name = TextEditingController();
  final _email = TextEditingController();

  @override
  void dispose() {
    _name.dispose();
    _email.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
        title: const Text('Someone you trust'),
        content: Column(mainAxisSize: MainAxisSize.min, children: [
          TextField(key: const Key('contact_name'), controller: _name, maxLength: 60, decoration: const InputDecoration(labelText: 'Their name')),
          TextField(key: const Key('contact_email'), controller: _email, keyboardType: TextInputType.emailAddress, decoration: const InputDecoration(labelText: 'Their email')),
          const SizedBox(height: 8),
          const Text('We email them once to ask if they agree. Nothing else is sent until they do.'),
        ]),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context), child: Text(AppLocalizations.of(context)?.sCancel ?? 'Cancel')),
          FilledButton(key: const Key('contact_save'), onPressed: () => Navigator.pop(context, (_name.text.trim(), _email.text.trim())), child: const Text('Ask them')),
        ],
      );
}

class _PlanSheet extends ConsumerStatefulWidget {
  const _PlanSheet({required this.contacts, this.matchId});
  final List<TrustedContact> contacts;
  final String? matchId;

  @override
  ConsumerState<_PlanSheet> createState() => _PlanSheetState();
}

class _PlanSheetState extends ConsumerState<_PlanSheet> {
  final _place = TextEditingController();
  late String _contact = widget.contacts.first.id;
  DateTime _meet = DateTime.now().add(const Duration(hours: 2));
  int _after = 120;
  bool _shareName = false;
  bool _busy = false;

  @override
  void dispose() {
    _place.dispose();
    super.dispose();
  }

  Future<void> _pickTime() async {
    final d = await showDatePicker(context: context, initialDate: _meet, firstDate: DateTime.now().subtract(const Duration(hours: 1)), lastDate: DateTime.now().add(const Duration(days: 30)));
    if (d == null || !mounted) return;
    final tm = await showTimePicker(context: context, initialTime: TimeOfDay.fromDateTime(_meet));
    if (tm == null) return;
    setState(() => _meet = DateTime(d.year, d.month, d.day, tm.hour, tm.minute));
  }

  Future<void> _send() async {
    setState(() => _busy = true);
    final ok = await guarded(
      context,
      () => ref.read(safetyRepositoryProvider).createPlan(
            contactId: _contact,
            place: _place.text.trim(),
            meetAt: _meet,
            checkinAfterMin: _after,
            matchId: widget.matchId,
            shareName: _shareName,
          ),
    );
    if (!mounted) return;
    setState(() => _busy = false);
    if (ok) Navigator.pop(context, true);
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return SafeArea(
      child: SingleChildScrollView(
        padding: EdgeInsets.fromLTRB(20, 4, 20, 20 + MediaQuery.viewInsetsOf(context).bottom),
        child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          Text(AppLocalizations.of(context)?.sShareADatePlan ?? 'Share a date plan', style: t.textTheme.headlineSmall),
          const SizedBox(height: 12),
          DropdownButtonFormField<String>(
            key: const Key('plan_contact'),
            initialValue: _contact,
            decoration: const InputDecoration(labelText: 'Send to'),
            items: [for (final c in widget.contacts) DropdownMenuItem(value: c.id, child: Text(c.name))],
            onChanged: (v) => setState(() => _contact = v ?? _contact),
          ),
          const SizedBox(height: 8),
          TextField(key: const Key('plan_place'), controller: _place, maxLength: 120, decoration: const InputDecoration(labelText: 'Where are you meeting?'), onChanged: (_) => setState(() {})),
          ListTile(key: const Key('plan_time'), contentPadding: EdgeInsets.zero, title: Text(AppLocalizations.of(context)?.sWhen ?? 'When'), trailing: Text(clock(_meet)), onTap: _pickTime),
          const SizedBox(height: 4),
          Text('Check in after', style: t.textTheme.labelLarge),
          const SizedBox(height: 4),
          Wrap(spacing: 8, children: [
            for (final m in const [60, 120, 240, 480])
              ChoiceChip(key: Key('after_$m'), label: Text('${m ~/ 60} h'), selected: _after == m, onSelected: (_) => setState(() => _after = m)),
          ]),
          if (widget.matchId != null)
            SwitchListTile(
              key: const Key('plan_share_name'),
              contentPadding: EdgeInsets.zero,
              title: const Text('Include their name'),
              subtitle: const Text('Only if they are shown in full; an anonymous match is never named'),
              value: _shareName,
              onChanged: (v) => setState(() => _shareName = v),
            ),
          const SizedBox(height: 8),
          FilledButton(key: const Key('plan_send'), onPressed: _busy || _place.text.trim().isEmpty ? null : _send, child: Text(AppLocalizations.of(context)?.sSendPlan ?? 'Send plan')),
        ]),
      ),
    );
  }
}

/// Where a trusted contact lands from the email: one tap to agree.
class ContactConfirmScreen extends ConsumerStatefulWidget {
  const ContactConfirmScreen({super.key, required this.token});
  final String token;

  @override
  ConsumerState<ContactConfirmScreen> createState() => _ContactConfirmScreenState();
}

class _ContactConfirmScreenState extends ConsumerState<ContactConfirmScreen> {
  String _state = 'ask'; // ask | done | failed
  bool _busy = false;

  Future<void> _agree() async {
    setState(() => _busy = true);
    try {
      await ref.read(safetyRepositoryProvider).confirm(widget.token);
      if (mounted) setState(() => _state = 'done');
    } catch (_) {
      if (mounted) setState(() => _state = 'failed');
    }
    if (mounted) setState(() => _busy = false);
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: Text(AppLocalizations.of(context)?.sTrustedContact ?? 'Trusted contact')),
      body: Padding(
        padding: const EdgeInsets.all(24),
        child: switch (_state) {
          'done' => Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              const Icon(Icons.verified_user_outlined, size: 48),
              const SizedBox(height: 12),
              Text('Thank you', key: const Key('confirm_done'), style: t.textTheme.headlineSmall),
              const SizedBox(height: 8),
              const Text('You will only hear from us when they share a plan with you. You can ignore any email later if you change your mind.'),
            ]),
          'failed' => Text('This link is not valid any more. Ask them to send a new request.', key: const Key('confirm_failed'), style: t.textTheme.bodyLarge),
          _ => Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text('Be someone\'s trusted contact?', style: t.textTheme.headlineSmall),
              const SizedBox(height: 8),
              const Text('A ConnectMe member named you as someone they trust. If you agree, they can send you the place and time of a date, and you will be told if they do not check in afterwards.'),
              const SizedBox(height: 16),
              FilledButton(key: const Key('confirm_agree'), onPressed: _busy ? null : _agree, child: Text(AppLocalizations.of(context)?.sIAgree ?? 'I agree')),
            ]),
        },
      ),
    );
  }
}


/// Text alerts for one contact: add a number, then enter the code the contact
/// reads out. Until that code is confirmed no text is ever sent to the number.
class _PhoneRow extends ConsumerWidget {
  const _PhoneRow({required this.contact, required this.repo});
  final TrustedContact contact;
  final SafetyRepository repo;

  Future<void> _ask(BuildContext context, WidgetRef ref, {required String title, required String hint, required String key, required Future<void> Function(String) onOk, TextInputType? type}) async {
    final v = await showDialog<String>(context: context, builder: (_) => _ValueDialog(title: title, hint: hint, fieldKey: key, type: type));
    if (v == null || v.isEmpty || !context.mounted) return;
    final ok = await guarded(context, () => onOk(v));
    if (ok) ref.invalidate(trustedContactsProvider);
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final t = Theme.of(context);
    final c = contact;
    if (c.phone.isEmpty) {
      return Align(
        alignment: Alignment.centerLeft,
        child: TextButton(
          key: Key('add_phone_${c.id}'),
          onPressed: () => _ask(context, ref,
              title: 'Phone number for texts',
              hint: '+14155550123',
              key: 'phone_field',
              type: TextInputType.phone,
              onOk: (v) => repo.setPhone(c.id, v)),
          child: const Text('Add a phone for text alerts'),
        ),
      );
    }
    if (!c.phoneConfirmed) {
      return Wrap(crossAxisAlignment: WrapCrossAlignment.center, spacing: 4, children: [
        Text('Code sent to ${c.phone}', style: t.textTheme.bodySmall),
        TextButton(
          key: Key('enter_code_${c.id}'),
          onPressed: () => _ask(context, ref,
              title: 'Enter the code',
              hint: '6 digits they read out to you',
              key: 'code_field',
              type: TextInputType.number,
              onOk: (v) => repo.confirmPhone(c.id, v)),
          child: const Text('Enter the code'),
        ),
        TextButton(key: Key('drop_phone_${c.id}'), onPressed: () async { if (await guarded(context, () => repo.removePhone(c.id))) ref.invalidate(trustedContactsProvider); }, child: const Text('Cancel')),
      ]);
    }
    return Wrap(crossAxisAlignment: WrapCrossAlignment.center, spacing: 4, children: [
      Text('Texts on: ${c.phone}', key: Key('texts_on_${c.id}'), style: t.textTheme.bodySmall),
      TextButton(key: Key('drop_phone_${c.id}'), onPressed: () async { if (await guarded(context, () => repo.removePhone(c.id))) ref.invalidate(trustedContactsProvider); }, child: const Text('Stop texts')),
    ]);
  }
}


/// A one-field dialog that owns its controller, so it is disposed only after
/// the dialog has finished closing.
class _ValueDialog extends StatefulWidget {
  const _ValueDialog({required this.title, required this.hint, required this.fieldKey, this.type});
  final String title;
  final String hint;
  final String fieldKey;
  final TextInputType? type;

  @override
  State<_ValueDialog> createState() => _ValueDialogState();
}

class _ValueDialogState extends State<_ValueDialog> {
  final _c = TextEditingController();

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(widget.title),
    content: TextField(key: Key(widget.fieldKey), controller: _c, autofocus: true, keyboardType: widget.type, decoration: InputDecoration(hintText: widget.hint)),
    actions: [
      TextButton(onPressed: () => Navigator.pop(context), child: const Text('Cancel')),
      FilledButton(key: const Key('phone_ok'), onPressed: () => Navigator.pop(context, _c.text.trim()), child: const Text('Continue')),
    ],
  );
}
