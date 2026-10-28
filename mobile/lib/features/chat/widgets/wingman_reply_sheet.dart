import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';

import '../../../shared/widgets/app_snack.dart';
import '../data/chat_models.dart';

/// Lets a member choose which recent messages to share with the AI wingman
/// and shows reply ideas to put in the message box. Nothing is sent until the
/// button is tapped, and nothing is pre-selected except the latest few.
class WingmanReplySheet extends StatefulWidget {
  const WingmanReplySheet({super.key, required this.messages, required this.load});
  final List<ChatMessage> messages;
  final Future<List<String>> Function(List<({bool mine, String text})> lines) load;

  @override
  State<WingmanReplySheet> createState() => _WingmanReplySheetState();
}

class _WingmanReplySheetState extends State<WingmanReplySheet> {
  late final Set<String> _chosen = {for (final m in widget.messages.reversed.take(3)) m.id};
  List<String>? _ideas;
  bool _busy = false;

  Future<void> _go() async {
    final lines = [for (final m in widget.messages) if (_chosen.contains(m.id)) (mine: m.fromMe, text: m.text.trim())];
    if (lines.isEmpty) return;
    setState(() => _busy = true);
    try {
      final r = await widget.load(lines);
      if (mounted) setState(() => _ideas = r);
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final ideas = _ideas;
    return SafeArea(
      child: ConstrainedBox(
        constraints: BoxConstraints(maxHeight: MediaQuery.of(context).size.height * 0.8),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 16, 16, 0),
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text(AppLocalizations.of(context)?.sAskWingmanForAReply ?? 'Ask Wingman for a reply', style: theme.textTheme.titleLarge),
                const SizedBox(height: 8),
                Text(
                  'Chats are private. Only the messages you tick below are sent to an AI assistant, without names, '
                  'and they are not saved. Messages from other people that you include are shared too, so tick only what you are comfortable sharing.',
                  style: theme.textTheme.bodySmall,
                ),
                if (ideas != null) ...[
                  const SizedBox(height: 8),
                  Text('Ideas written by an AI assistant. Edit before you send.', style: theme.textTheme.bodySmall),
                ],
              ]),
            ),
            Flexible(
              child: ListView(
                shrinkWrap: true,
                children: ideas == null
                    ? [
                        for (final m in widget.messages)
                          CheckboxListTile(
                            key: Key('share_${m.id}'),
                            dense: true,
                            value: _chosen.contains(m.id),
                            onChanged: _busy ? null : (v) => setState(() => v == true ? _chosen.add(m.id) : _chosen.remove(m.id)),
                            title: Text(m.text, maxLines: 2, overflow: TextOverflow.ellipsis),
                            subtitle: Text(m.fromMe ? 'You' : 'Someone else'),
                          ),
                      ]
                    : [
                        for (var i = 0; i < ideas.length; i++)
                          ListTile(key: Key('reply_idea_$i'), title: Text(ideas[i]), onTap: () => Navigator.pop(context, ideas[i])),
                      ],
              ),
            ),
            if (ideas == null)
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
                child: FilledButton(
                  key: const Key('get_reply_ideas'),
                  onPressed: _busy || _chosen.isEmpty ? null : _go,
                  child: Text(_busy ? 'Thinking…' : 'Get reply ideas'),
                ),
              ),
          ],
        ),
      ),
    );
  }
}
