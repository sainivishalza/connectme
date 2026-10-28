import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter/services.dart';

import '../data/chat_models.dart';

/// The quick reactions offered on a message.
const quickReactions = ['👍', '❤️', '😂', '😮', '😢', '🙏'];

/// What the member chose from the message menu.
sealed class MessageAction {
  const MessageAction();
}

class ReactAction extends MessageAction {
  const ReactAction(this.emoji);
  final String emoji;
}

class ReplyAction extends MessageAction {
  const ReplyAction();
}

class UnsendAction extends MessageAction {
  const UnsendAction();
}

/// A sheet over a message: react with an emoji, reply, copy the text, or (on
/// your own messages) unsend it for everyone. Copy happens here; the rest are
/// returned for the chat to carry out.
Future<MessageAction?> showMessageMenu(BuildContext context, ChatMessage m) {
  return showModalBottomSheet<MessageAction>(
    context: context,
    useRootNavigator: true,
    showDragHandle: true,
    builder: (c) => SafeArea(
      child: Column(mainAxisSize: MainAxisSize.min, children: [
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 12),
          child: Row(mainAxisAlignment: MainAxisAlignment.spaceEvenly, children: [
            for (final e in quickReactions)
              InkResponse(
                key: Key('react_$e'),
                radius: 28,
                onTap: () => Navigator.pop(c, ReactAction(e)),
                child: Padding(
                  padding: const EdgeInsets.all(8),
                  child: Text(e, style: TextStyle(fontSize: 30, fontWeight: m.reactions['me'] == e ? FontWeight.w900 : FontWeight.w400)),
                ),
              ),
          ]),
        ),
        const Divider(),
        ListTile(key: const Key('menu_reply'), leading: const Icon(Icons.reply), title: Text(AppLocalizations.of(context)?.sReply ?? 'Reply'), onTap: () => Navigator.pop(c, const ReplyAction())),
        if (m.kind == MsgKind.text && m.text.isNotEmpty)
          ListTile(
            key: const Key('menu_copy'),
            leading: const Icon(Icons.copy_outlined),
            title: Text(AppLocalizations.of(context)?.sCopy ?? 'Copy'),
            onTap: () async {
              await Clipboard.setData(ClipboardData(text: m.text));
              if (c.mounted) Navigator.pop(c);
            },
          ),
        if (m.fromMe)
          ListTile(
            key: const Key('menu_unsend'),
            leading: Icon(Icons.undo, color: Theme.of(c).colorScheme.error),
            title: Text('Unsend for everyone', style: TextStyle(color: Theme.of(c).colorScheme.error)),
            onTap: () => Navigator.pop(c, const UnsendAction()),
          ),
      ]),
    ),
  );
}
