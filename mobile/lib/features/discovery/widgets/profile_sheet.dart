import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';

import '../../../core/models.dart';
import '../../follows/follow_button.dart';
import 'profile_card.dart';

/// The whole profile in a sheet that slides up from the deck: all photos, the
/// facts, prompts, voice intro and video, with the same privacy the server
/// applied to the card.
Future<void> showProfileSheet(BuildContext context, PublicProfile profile, {Widget? actions}) {
  return showModalBottomSheet<void>(
    context: context, useRootNavigator: true,
    isScrollControlled: true,
    showDragHandle: true,
    useSafeArea: true,
    builder: (c) => DraggableScrollableSheet(
      expand: false,
      initialChildSize: 0.92,
      minChildSize: 0.5,
      maxChildSize: 0.97,
      builder: (context, scroll) => ListView(
        key: const Key('profile_sheet'),
        controller: scroll,
        padding: const EdgeInsets.fromLTRB(12, 0, 12, 24),
        children: [
          ProfileCard(profile: profile),
          if (!profile.anonymous) ...[const SizedBox(height: 12), FollowButton(profile: profile)],
          if (actions != null) ...[const SizedBox(height: 12), actions],
        ],
      ),
    ),
  );
}

/// "Say something with your like": a one-line note that goes with the like.
/// The other person sees it next to your profile, and in the chat if you match.
class LikeWithNote extends StatefulWidget {
  const LikeWithNote({super.key, required this.onSend});
  final void Function(String note) onSend;

  @override
  State<LikeWithNote> createState() => _LikeWithNoteState();
}

class _LikeWithNoteState extends State<LikeWithNote> {
  final _text = TextEditingController();

  @override
  void dispose() {
    _text.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Text('Say something with your like', style: t.textTheme.titleSmall),
          const SizedBox(height: 8),
          TextField(
            key: const Key('like_note'),
            controller: _text,
            maxLength: 140,
            decoration: const InputDecoration(hintText: 'Your hiking photo made me smile'),
            onChanged: (_) => setState(() {}),
          ),
          Align(
            alignment: Alignment.centerRight,
            child: FilledButton.icon(
              key: const Key('send_like_note'),
              onPressed: _text.text.trim().isEmpty ? null : () => widget.onSend(_text.text.trim()),
              icon: const Icon(Icons.favorite),
              label: Text(AppLocalizations.of(context)?.sSendLike ?? 'Send like'),
            ),
          ),
        ]),
      ),
    );
  }
}
