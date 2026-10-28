import 'package:flutter/material.dart';
import '../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../shared/widgets/app_snack.dart';
import '../profile/profile_repository.dart';

class _Step {
  const _Step(
    this.key,
    this.icon,
    this.title,
    this.body,
    this.route,
    this.action,
  );
  final String key;
  final IconData icon;
  final String title;
  final String body;
  final String route;
  final String action;
}

const _steps = [
  _Step(
    'photos',
    Icons.photo_camera_outlined,
    'Add your photos',
    'People decide fast. Two or three clear photos make the biggest difference, and you choose when anyone sees them.',
    '/profile/edit',
    'Add photos',
  ),
  _Step(
    'about',
    Icons.favorite_border,
    'What are you looking for?',
    'Tell us your goal and a few lifestyle answers. They help us suggest people who want the same things.',
    '/profile/about',
    'Answer now',
  ),
  _Step(
    'prompts',
    Icons.chat_bubble_outline,
    'Say something about you',
    'Pick a prompt and answer in a line. It gives people an easy way to start a conversation.',
    '/profile/prompts',
    'Pick a prompt',
  ),
  _Step(
    'quiz',
    Icons.psychology_alt_outlined,
    'Take the compatibility quiz',
    'Answer a few quick questions and see how well you match with people before you see a photo.',
    '/profile/quiz',
    'Start the quiz',
  ),
  _Step(
    'notifications',
    Icons.notifications_outlined,
    'Choose your notifications',
    'Plain alerts, no names or message text. Set quiet hours so nothing wakes you.',
    '/settings/notifications',
    'Set up',
  ),
];

/// A short guided start for new members. Every step is optional and opens the
/// real screen for it; finishing or skipping marks the welcome as seen.
class WelcomeScreen extends ConsumerStatefulWidget {
  const WelcomeScreen({super.key});

  @override
  ConsumerState<WelcomeScreen> createState() => _WelcomeScreenState();
}

class _WelcomeScreenState extends ConsumerState<WelcomeScreen> {
  int _i = 0;
  bool _busy = false;

  Future<void> _finish() async {
    setState(() => _busy = true);
    await guarded(
      context,
      () => ref.read(profileRepositoryProvider).markOnboarded(),
    );
    ref.invalidate(meProvider);
    if (mounted) context.go('/discover');
  }

  Future<void> _open(_Step s) async {
    await context.push(s.route);
    ref.invalidate(meProvider);
    if (mounted) _next();
  }

  void _next() {
    if (_i >= _steps.length - 1) {
      _finish();
    } else {
      setState(() => _i++);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final s = _steps[_i];
    final me = ref.watch(meProvider).asData?.value;
    return Scaffold(
      appBar: AppBar(
        automaticallyImplyLeading: false,
        title: Text(AppLocalizations.of(context)?.sWelcome ?? 'Welcome'),
        actions: [
          TextButton(
            key: const Key('skip_all'),
            onPressed: _busy ? null : _finish,
            child: Text(AppLocalizations.of(context)?.sSkipForNow ?? 'Skip for now'),
          ),
        ],
      ),
      body: SafeArea(
        // Scrolls when the text is large, so nothing is ever cut off.
        child: LayoutBuilder(
          builder: (context, box) => SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: ConstrainedBox(
              constraints: BoxConstraints(minHeight: box.maxHeight - 48),
              child: IntrinsicHeight(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        for (var k = 0; k < _steps.length; k++)
                          Expanded(
                            child: Container(
                              key: Key('dot_$k'),
                              height: 6,
                              margin: const EdgeInsets.only(right: 6),
                              decoration: BoxDecoration(
                                borderRadius: BorderRadius.circular(3),
                                color: k <= _i
                                    ? t.colorScheme.primary
                                    : t.colorScheme.surfaceContainerHighest,
                              ),
                            ),
                          ),
                      ],
                    ),
                    const SizedBox(height: 8),
                    Text(
                      'Step ${_i + 1} of ${_steps.length}',
                      style: t.textTheme.labelLarge,
                    ),
                    const Spacer(),
                    Icon(s.icon, size: 56, color: t.colorScheme.primary),
                    const SizedBox(height: 20),
                    Text(
                      s.title,
                      key: const Key('welcome_title'),
                      style: t.textTheme.headlineMedium,
                    ),
                    const SizedBox(height: 12),
                    Text(s.body, style: t.textTheme.bodyLarge),
                    if (me != null && me.completeness.percent > 0) ...[
                      const SizedBox(height: 16),
                      Text(
                        'Your profile is ${me.completeness.percent}% complete',
                        key: const Key('welcome_percent'),
                        style: t.textTheme.bodySmall,
                      ),
                    ],
                    const Spacer(),
                    FilledButton(
                      key: const Key('welcome_do'),
                      onPressed: _busy ? null : () => _open(s),
                      child: Text(s.action),
                    ),
                    const SizedBox(height: 8),
                    OutlinedButton(
                      key: const Key('welcome_next'),
                      onPressed: _busy ? null : _next,
                      child: Text(
                        _i == _steps.length - 1 ? 'Start exploring' : 'Not now',
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// Sends a member who has not seen the welcome to it, once per app start.
class OnboardingGate extends ConsumerStatefulWidget {
  const OnboardingGate({super.key, required this.child});
  final Widget child;

  @override
  ConsumerState<OnboardingGate> createState() => _OnboardingGateState();
}

bool _shownThisSession = false;

class _OnboardingGateState extends ConsumerState<OnboardingGate> {
  @override
  Widget build(BuildContext context) {
    final me = ref.watch(meProvider).asData?.value;
    if (me != null && !me.onboarded && !_shownThisSession) {
      _shownThisSession = true;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) context.push('/welcome');
      });
    }
    return widget.child;
  }
}

/// For tests: forget that the welcome was shown.
void resetOnboardingGateForTest() => _shownThisSession = false;
