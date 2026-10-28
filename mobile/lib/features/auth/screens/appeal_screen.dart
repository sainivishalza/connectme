import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/providers.dart';
import '../../../shared/widgets/app_snack.dart';

/// Where a banned member asks for the ban to be looked at again. They cannot
/// sign in, so the form proves the account is theirs with the email and
/// password; staff read the message and answer by lifting the ban or keeping it.
class AppealScreen extends ConsumerStatefulWidget {
  const AppealScreen({super.key});

  @override
  ConsumerState<AppealScreen> createState() => _AppealScreenState();
}

class _AppealScreenState extends ConsumerState<AppealScreen> {
  final _email = TextEditingController();
  final _password = TextEditingController();
  final _message = TextEditingController();
  bool _busy = false;
  bool _sent = false;

  @override
  void dispose() {
    _email.dispose();
    _password.dispose();
    _message.dispose();
    super.dispose();
  }

  bool get _valid => _email.text.contains('@') && _password.text.isNotEmpty && _message.text.trim().length >= 10;

  Future<void> _send() async {
    setState(() => _busy = true);
    final ok = await guarded(context, () => ref.read(apiClientProvider).post('/moderation/appeals', body: {
      'email': _email.text.trim(),
      'password': _password.text,
      'message': _message.text.trim(),
    }));
    if (!mounted) return;
    setState(() {
      _busy = false;
      _sent = ok;
    });
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        title: const Text('Appeal a ban'),
        leading: BackButton(onPressed: () => context.canPop() ? context.pop() : context.go('/login')),
      ),
      body: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          if (_sent) ...[
            Text('Your appeal was sent', key: const Key('appeal_sent'), style: t.textTheme.titleLarge),
            const SizedBox(height: 8),
            const Text('A person will read it. Sign in to see whether the ban was lifted; we do not send a separate message.'),
          ] else ...[
            const Text(
              'If you think your account was banned by mistake, tell us why. '
              'Enter the email and password of the banned account so we know it is yours.',
            ),
            const SizedBox(height: 16),
            TextField(
              key: const Key('appeal_email'),
              controller: _email,
              keyboardType: TextInputType.emailAddress,
              autofillHints: const [AutofillHints.email],
              decoration: const InputDecoration(labelText: 'Email'),
              onChanged: (_) => setState(() {}),
            ),
            const SizedBox(height: 12),
            TextField(
              key: const Key('appeal_password'),
              controller: _password,
              obscureText: true,
              decoration: const InputDecoration(labelText: 'Password'),
              onChanged: (_) => setState(() {}),
            ),
            const SizedBox(height: 12),
            TextField(
              key: const Key('appeal_message'),
              controller: _message,
              maxLines: 5,
              maxLength: 1000,
              decoration: const InputDecoration(labelText: 'Why should the ban be lifted?', alignLabelWithHint: true),
              onChanged: (_) => setState(() {}),
            ),
            const SizedBox(height: 12),
            FilledButton(key: const Key('appeal_send'), onPressed: _valid && !_busy ? _send : null, child: const Text('Send appeal')),
          ],
        ],
      ),
    );
  }
}
