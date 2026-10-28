import 'dart:typed_data';

import 'package:flutter/material.dart';

import '../../../core/theme/tokens.dart';
import '../data/chat_models.dart';
import '../voice/voice_bubble.dart';

/// One chat message. Image attachments are loaded lazily through
/// [loadImage], which downloads and decrypts them.
class MessageBubble extends StatelessWidget {
  const MessageBubble({super.key, required this.message, this.loadImage, this.onLongPress, this.onDoubleTap});

  /// Long-press opens the message menu (react, reply, copy, unsend).
  final VoidCallback? onLongPress;

  /// Double-tap sends a quick heart.
  final VoidCallback? onDoubleTap;

  final ChatMessage message;
  final Future<Uint8List> Function(ChatMessage)? loadImage;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final mine = message.fromMe;
    final bg = mine
        ? t.colorScheme.primary
        : t.colorScheme.surfaceContainerHighest;
    // Your messages carry the app's own light (coral to pink), so a thread reads
    // as two voices at a glance; theirs stay quiet.
    final fg = mine ? ModeLight.daylight.ink : t.colorScheme.onSurface;

    Widget body;
    switch (message.kind) {
      case MsgKind.text:
        body = Text(
          message.text,
          style: t.textTheme.bodyLarge?.copyWith(color: fg),
        );
      case MsgKind.image:
        body = _Image(message: message, load: loadImage, fg: fg);
      case MsgKind.voice:
        body = VoiceBubbleBody(message: message, load: loadImage, color: fg);
      case MsgKind.control:
        return const SizedBox.shrink(); // reactions and unsend notices are never bubbles
      case MsgKind.locked:
        body = Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.lock_outline,
              size: 16,
              color: fg.withValues(alpha: 0.7),
            ),
            const SizedBox(width: 6),
            Flexible(
              child: Text(
                message.text,
                style: TextStyle(
                  color: fg.withValues(alpha: 0.7),
                  fontStyle: FontStyle.italic,
                ),
              ),
            ),
          ],
        );
    }

    return Align(
      alignment: mine ? Alignment.centerRight : Alignment.centerLeft,
      child: Column(
        crossAxisAlignment: mine ? CrossAxisAlignment.end : CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          GestureDetector(
            behavior: HitTestBehavior.opaque,
            onLongPress: onLongPress,
            onDoubleTap: onDoubleTap,
            child: _bubble(context, t, mine, bg, fg, body),
          ),
          if (message.reactions.isNotEmpty) _Reactions(reactions: message.reactions, mine: mine),
        ],
      ),
    );
  }

  Widget _bubble(BuildContext context, ThemeData t, bool mine, Color bg, Color fg, Widget body) {
    return Container(
        constraints: BoxConstraints(
          maxWidth: MediaQuery.of(context).size.width * 0.78,
        ),
        margin: const EdgeInsets.symmetric(vertical: 4),
        padding: const EdgeInsets.symmetric(horizontal: 15, vertical: 10),
        decoration: BoxDecoration(
          color: mine ? null : bg,
          gradient: mine ? ModeLight.daylight.gradient : null,
          borderRadius: BorderRadius.only(
            topLeft: const Radius.circular(22),
            topRight: const Radius.circular(22),
            bottomLeft: Radius.circular(mine ? 22 : 6),
            bottomRight: Radius.circular(mine ? 6 : 22),
          ),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.end,
          mainAxisSize: MainAxisSize.min,
          children: [
            if (message.replyToId != null && message.replyQuote.isNotEmpty) _QuoteBlock(text: message.replyQuote, fg: fg, mine: mine),
            Align(alignment: Alignment.centerLeft, child: body),
            const SizedBox(height: 2),
            Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                if (message.burnAfterS > 0)
                  Icon(
                    Icons.local_fire_department,
                    size: 12,
                    color: fg.withValues(alpha: 0.7),
                  ),
                const SizedBox(width: 2),
                Text(
                  _time(message.createdAt),
                  style: TextStyle(
                    fontSize: 11,
                    color: fg.withValues(alpha: 0.75),
                  ),
                ),
                if (mine) ...[
                  const SizedBox(width: 4),
                  Icon(
                    message.pending
                        ? Icons.schedule
                        : (message.readAt != null
                              ? Icons.done_all
                              : Icons.done),
                    size: 12,
                    color: fg.withValues(alpha: 0.7),
                  ),
                ],
              ],
            ),
          ],
        ),
      );
  }

  static String _time(DateTime d) =>
      '${d.hour.toString().padLeft(2, '0')}:${d.minute.toString().padLeft(2, '0')}';
}

class _Image extends StatefulWidget {
  const _Image({required this.message, required this.load, required this.fg});
  final ChatMessage message;
  final Future<Uint8List> Function(ChatMessage)? load;
  final Color fg;
  @override
  State<_Image> createState() => _ImageState();
}

class _ImageState extends State<_Image> {
  Future<Uint8List>? _f;

  @override
  void initState() {
    super.initState();
    _f = widget.load?.call(widget.message);
  }

  @override
  Widget build(BuildContext context) {
    final f = _f;
    if (f == null) return Icon(Icons.image_outlined, color: widget.fg);
    return FutureBuilder<Uint8List>(
      future: f,
      builder: (c, s) {
        if (s.hasError) {
          return Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(Icons.broken_image_outlined, color: widget.fg),
              const SizedBox(width: 6),
              Text('Photo unavailable', style: TextStyle(color: widget.fg)),
            ],
          );
        }
        if (!s.hasData) {
          return const SizedBox(
            width: 120,
            height: 120,
            child: Center(child: CircularProgressIndicator(strokeWidth: 2)),
          );
        }
        return ClipRRect(
          borderRadius: BorderRadius.circular(10),
          child: Image.memory(s.data!, width: 220, fit: BoxFit.cover),
        );
      },
    );
  }
}

/// The line quoted from the message being replied to.
class _QuoteBlock extends StatelessWidget {
  const _QuoteBlock({required this.text, required this.fg, required this.mine});
  final String text;
  final Color fg;
  final bool mine;

  @override
  Widget build(BuildContext context) {
    return Container(
      key: const Key('reply_quote'),
      margin: const EdgeInsets.only(bottom: 6),
      padding: const EdgeInsets.fromLTRB(10, 6, 10, 6),
      decoration: BoxDecoration(
        color: fg.withValues(alpha: 0.14),
        borderRadius: BorderRadius.circular(10),
        border: Border(left: BorderSide(color: fg.withValues(alpha: 0.6), width: 3)),
      ),
      child: Text(text, maxLines: 2, overflow: TextOverflow.ellipsis, style: TextStyle(color: fg.withValues(alpha: 0.85), fontSize: 13)),
    );
  }
}

/// Emoji under a bubble: one from each person at most.
class _Reactions extends StatelessWidget {
  const _Reactions({required this.reactions, required this.mine});
  final Map<String, String> reactions;
  final bool mine;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final emojis = [if (reactions['them'] != null) reactions['them']!, if (reactions['me'] != null) reactions['me']!];
    return Transform.translate(
      offset: const Offset(0, -6),
      child: Container(
        key: const Key('reactions'),
        margin: EdgeInsets.only(left: mine ? 0 : 12, right: mine ? 12 : 0),
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
        decoration: BoxDecoration(
          color: t.colorScheme.surface,
          borderRadius: BorderRadius.circular(14),
          border: Border.all(color: t.colorScheme.outlineVariant),
        ),
        child: Text(emojis.join(' '), style: const TextStyle(fontSize: 15)),
      ),
    );
  }
}
