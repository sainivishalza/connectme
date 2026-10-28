import 'package:flutter/material.dart';

import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/validators.dart';
import '../../../shared/widgets/app_button.dart';
import '../../../shared/widgets/app_snack.dart';
import '../auth_controller.dart';

/// Verifies an email with the token from the verification email.
class VerifyScreen extends ConsumerStatefulWidget {
  const VerifyScreen({super.key, this.mode = VerifyMode.email});
  final VerifyMode mode;
  @override
  ConsumerState<VerifyScreen> createState() => _VerifyScreenState();
}

enum VerifyMode { email, resetPassword }

class _VerifyScreenState extends ConsumerState<VerifyScreen> {
  final _form = GlobalKey<FormState>();
  final _token = TextEditingController();
  final _password = TextEditingController();
  bool _busy = false;

  @override
  void dispose() {
    _token.dispose();
    _password.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (!_form.currentState!.validate()) return;
    setState(() => _busy = true);
    final c = ref.read(authControllerProvider.notifier);
    final ok = await guarded(
      context,
      () => widget.mode == VerifyMode.email
          ? c.verifyEmail(_token.text)
          : c.resetPassword(_token.text, _password.text),
    );
    if (!mounted) return;
    setState(() => _busy = false);
    if (ok) {
      showMessage(context, widget.mode == VerifyMode.email ? 'Email verified. You can sign in.' : 'Password updated. Sign in.');
      context.go('/login');
    }
  }

  @override
  Widget build(BuildContext context) {
    final reset = widget.mode == VerifyMode.resetPassword;
    return Scaffold(
      appBar: AppBar(title: Text(reset ? 'Reset password' : 'Verify email'), leading: BackButton(onPressed: () => context.go('/login'))),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(24),
          child: Form(
            key: _form,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(reset ? 'Paste the code from the reset email and choose a new password.' : 'Paste the code from the verification email.'),
                const SizedBox(height: 16),
                TextFormField(
                  key: const Key('token'),
                  controller: _token,
                  decoration: const InputDecoration(labelText: 'Code'),
                  validator: (v) => Validators.required(v, 'Code'),
                ),
                if (reset) ...[
                  const SizedBox(height: 16),
                  TextFormField(
                    key: const Key('new_password'),
                    controller: _password,
                    obscureText: true,
                    decoration: const InputDecoration(labelText: 'New password'),
                    validator: Validators.password,
                  ),
                ],
                const SizedBox(height: 24),
                AppButton(label: reset ? 'Update password' : 'Verify', busy: _busy, onPressed: _submit),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// Requests a password-reset email, then moves to the reset form.
class ForgotScreen extends ConsumerStatefulWidget {
  const ForgotScreen({super.key});
  @override
  ConsumerState<ForgotScreen> createState() => _ForgotScreenState();
}

class _ForgotScreenState extends ConsumerState<ForgotScreen> {
  final _email = TextEditingController();
  final _form = GlobalKey<FormState>();
  bool _busy = false;

  @override
  void dispose() {
    _email.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (!_form.currentState!.validate()) return;
    setState(() => _busy = true);
    final ok = await guarded(context, () => ref.read(authControllerProvider.notifier).forgotPassword(_email.text));
    if (!mounted) return;
    setState(() => _busy = false);
    if (ok) {
      showMessage(context, 'If the account exists, we sent an email.');
      context.go('/reset');
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
        appBar: AppBar(title: Text(AppLocalizations.of(context)?.forgotTitle ?? 'Forgot password'), leading: BackButton(onPressed: () => context.go('/login'))),
        body: Padding(
          padding: const EdgeInsets.all(24),
          child: Form(
            key: _form,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                TextFormField(
                  controller: _email,
                  keyboardType: TextInputType.emailAddress,
                  decoration: InputDecoration(labelText: AppLocalizations.of(context)?.loginEmail ?? 'Email'),
                  validator: Validators.email,
                ),
                const SizedBox(height: 24),
                AppButton(label: AppLocalizations.of(context)?.forgotSend ?? 'Send reset email', busy: _busy, onPressed: _submit),
              ],
            ),
          ),
        ),
      );
}
