import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/validators.dart';
import '../../../l10n/app_localizations.dart';
import '../../../shared/widgets/app_button.dart';
import '../../../shared/widgets/app_snack.dart';
import '../auth_controller.dart';

class RegisterScreen extends ConsumerStatefulWidget {
  const RegisterScreen({super.key});
  @override
  ConsumerState<RegisterScreen> createState() => _RegisterScreenState();
}

class _RegisterScreenState extends ConsumerState<RegisterScreen> {
  final _form = GlobalKey<FormState>();
  final _email = TextEditingController();
  final _password = TextEditingController();
  final _phone = TextEditingController();
  DateTime? _dob;
  bool _busy = false;
  bool _dobError = false;

  @override
  void dispose() {
    _email.dispose();
    _password.dispose();
    _phone.dispose();
    super.dispose();
  }

  Future<void> _pickDob() async {
    final now = DateTime.now();
    final latest = DateTime(now.year - 18, now.month, now.day).subtract(const Duration(days: 1));
    final d = await showDatePicker(
      context: context,
      initialDate: _dob ?? latest,
      firstDate: DateTime(now.year - 100),
      lastDate: latest,
      helpText: 'Date of birth (18+ only)',
    );
    if (d != null) setState(() => _dob = d);
  }

  Future<void> _submit() async {
    final ok = _form.currentState!.validate();
    final adult = _dob != null && Validators.isAdult(_dob!);
    setState(() => _dobError = !adult);
    if (!ok || !adult) return;
    setState(() => _busy = true);
    final done = await guarded(
      context,
      () => ref.read(authControllerProvider.notifier).register(
            email: _email.text,
            password: _password.text,
            dateOfBirth: Validators.isoDate(_dob!),
            phone: _phone.text,
          ),
    );
    if (!mounted) return;
    setState(() => _busy = false);
    if (done) {
      showMessage(context, 'Check your email for the verification code.');
      context.go('/verify');
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: Text(AppLocalizations.of(context)?.regTitle ?? 'Create account'), leading: BackButton(onPressed: () => context.go('/login'))),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(24),
          child: Form(
            key: _form,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                TextFormField(
                  key: const Key('email'),
                  controller: _email,
                  keyboardType: TextInputType.emailAddress,
                  decoration: InputDecoration(labelText: AppLocalizations.of(context)?.loginEmail ?? 'Email'),
                  validator: Validators.email,
                ),
                const SizedBox(height: 16),
                TextFormField(
                  key: const Key('password'),
                  controller: _password,
                  obscureText: true,
                  decoration: InputDecoration(
                    labelText: AppLocalizations.of(context)?.loginPassword ?? 'Password',
                    helperText: '12+ characters with upper, lower, number and symbol',
                    helperMaxLines: 2,
                  ),
                  validator: Validators.password,
                ),
                const SizedBox(height: 16),
                TextFormField(
                  controller: _phone,
                  keyboardType: TextInputType.phone,
                  decoration: InputDecoration(labelText: AppLocalizations.of(context)?.regPhone ?? 'Phone (optional, +14155550123)'),
                ),
                const SizedBox(height: 16),
                InkWell(
                  key: const Key('dob'),
                  onTap: _pickDob,
                  child: InputDecorator(
                    decoration: InputDecoration(
                      labelText: AppLocalizations.of(context)?.regDob ?? 'Date of birth',
                      errorText: _dobError ? 'You must be 18 or older' : null,
                      suffixIcon: const Icon(Icons.calendar_today),
                    ),
                    child: Text(_dob == null ? 'Select' : Validators.isoDate(_dob!)),
                  ),
                ),
                const SizedBox(height: 8),
                Text(AppLocalizations.of(context)?.regAdults ?? 'ConnectMe is for adults only.', style: t.textTheme.bodySmall),
                const SizedBox(height: 24),
                AppButton(key: const Key('register'), label: AppLocalizations.of(context)?.regTitle ?? 'Create account', busy: _busy, onPressed: _submit),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
