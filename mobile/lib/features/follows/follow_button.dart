import 'package:flutter/material.dart';
import '../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../shared/widgets/app_snack.dart';
import 'follows_repository.dart';

/// Follow / Following toggle for a profile shown in full, with the follower
/// count. Renders nothing for anonymous profiles: nobody can follow someone
/// they cannot see.
class FollowButton extends ConsumerStatefulWidget {
  const FollowButton({super.key, required this.profile});
  final PublicProfile profile;

  @override
  ConsumerState<FollowButton> createState() => _FollowButtonState();
}

class _FollowButtonState extends ConsumerState<FollowButton> {
  late bool _following = widget.profile.following;
  late int _followers = widget.profile.followers;
  bool _busy = false;

  Future<void> _toggle() async {
    final want = !_following;
    setState(() => _busy = true);
    final ok = await guarded(context, () async {
      final repo = ref.read(followsRepositoryProvider);
      want ? await repo.follow(widget.profile.ref) : await repo.unfollow(widget.profile.ref);
    });
    if (!mounted) return;
    setState(() {
      _busy = false;
      if (ok) {
        _following = want;
        _followers = (_followers + (want ? 1 : -1)).clamp(0, 1 << 30);
      }
    });
    if (ok) ref.invalidate(followListProvider);
  }

  @override
  Widget build(BuildContext context) {
    if (widget.profile.anonymous) return const SizedBox.shrink();
    final t = Theme.of(context);
    return Row(children: [
      _following
          ? OutlinedButton.icon(
              key: const Key('unfollow'),
              onPressed: _busy ? null : _toggle,
              icon: const Icon(Icons.check),
              label: Text(AppLocalizations.of(context)?.profileFollowing ?? 'Following'),
            )
          : FilledButton.tonalIcon(
              key: const Key('follow'),
              onPressed: _busy ? null : _toggle,
              icon: const Icon(Icons.person_add_alt),
              label: Text(AppLocalizations.of(context)?.sFollow ?? 'Follow'),
            ),
      const SizedBox(width: 12),
      Text(_followers == 1 ? '1 follower' : '$_followers followers', key: const Key('follower_count'), style: t.textTheme.bodyMedium),
    ]);
  }
}
