import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models.dart';
import '../../../core/theme/tokens.dart';
import '../../../shared/widgets/avatar.dart';

/// "It's a match": a full-screen moment, not a dialog. Shows the other person
/// (photo only when the server allowed one), what they have in common when we
/// know, and two ways forward.
Future<void> showMatchOverlay(BuildContext context, {required PublicProfile profile, required VoidCallback onSayHi}) {
  return showGeneralDialog<void>(
    context: context,
    barrierDismissible: true,
    barrierLabel: 'Close',
    barrierColor: Colors.black.withValues(alpha: 0.78),
    transitionDuration: const Duration(milliseconds: 280),
    transitionBuilder: (c, a, _, child) => FadeTransition(opacity: a, child: ScaleTransition(scale: Tween(begin: 0.92, end: 1.0).animate(CurvedAnimation(parent: a, curve: Curves.easeOutBack)), child: child)),
    pageBuilder: (c, _, _) => _MatchView(profile: profile, onSayHi: onSayHi),
  );
}

class _MatchView extends ConsumerWidget {
  const _MatchView({required this.profile, required this.onSayHi});
  final PublicProfile profile;
  final VoidCallback onSayHi;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final t = Theme.of(context);
    final p = profile;
    final path = p.photos.isEmpty ? null : p.photos.first;
    final photo = path == null ? null : ref.watch(photoBytesProvider(path)).maybeWhen(data: (b) => b, orElse: () => null);
    final light = ModeLight.of(p.anonymous && p.mode == 'open' ? 'incognito' : p.mode);
    return Material(
      type: MaterialType.transparency,
      child: SafeArea(
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 420),
            child: Padding(
              padding: const EdgeInsets.all(28),
              child: Column(mainAxisSize: MainAxisSize.min, children: [
                Text("It's a match!", key: const Key('match_title'), style: t.textTheme.displaySmall?.copyWith(color: Colors.white, fontWeight: FontWeight.w800)),
                const SizedBox(height: 24),
                Container(
                  width: 168,
                  height: 168,
                  decoration: BoxDecoration(
                    shape: BoxShape.circle,
                    gradient: light.gradient,
                    border: Border.all(color: Colors.white, width: 4),
                    image: photo == null ? null : DecorationImage(image: MemoryImage(photo), fit: BoxFit.cover),
                  ),
                  child: photo == null ? Center(child: Avatar(name: p.displayName, anonymous: p.anonymous, radius: 52, mode: p.mode)) : null,
                ),
                const SizedBox(height: 18),
                Text(p.anonymous ? 'Someone anonymous likes you back' : '${p.title} likes you back',
                    textAlign: TextAlign.center, style: t.textTheme.titleLarge?.copyWith(color: Colors.white)),
                const SizedBox(height: 8),
                Text(
                  p.anonymous ? 'You can reveal yourselves to each other whenever you both want.' : 'Say hello while the moment is fresh.',
                  textAlign: TextAlign.center,
                  style: t.textTheme.bodyMedium?.copyWith(color: Colors.white.withValues(alpha: 0.85)),
                ),
                const SizedBox(height: 28),
                SizedBox(
                  width: double.infinity,
                  child: FilledButton(
                    key: const Key('match_say_hi'),
                    onPressed: () {
                      Navigator.of(context).pop();
                      onSayHi();
                    },
                    child: Text(AppLocalizations.of(context)?.sSayHi ?? 'Say hi'),
                  ),
                ),
                const SizedBox(height: 10),
                TextButton(
                  key: const Key('match_keep_swiping'),
                  onPressed: () => Navigator.of(context).pop(),
                  child: const Text('Keep swiping', style: TextStyle(color: Colors.white)),
                ),
              ]),
            ),
          ),
        ),
      ),
    );
  }
}
