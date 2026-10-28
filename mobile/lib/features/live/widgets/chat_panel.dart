import 'package:flutter/material.dart';

import '../../../core/theme/tokens.dart';
import '../live_repository.dart';

/// A light, readable colour for a person's name, the same for the same name.
Color _nameColour(String name) {
  const hues = [Color(0xFFFFB199), Color(0xFFD9B8FF), Color(0xFF9FD3FF), Color(0xFF8FE3D2), Color(0xFFFFD98A), Color(0xFFB7E8A0), Color(0xFFFFA8C9)];
  return hues[name.runes.fold<int>(0, (a, r) => a + r) % hues.length];
}

/// Recent chat lines over the video plus a one-line composer.
class ChatPanel extends StatefulWidget {
  const ChatPanel({
    super.key,
    required this.lines,
    required this.onSend,
    this.onLineTap,
  });
  final List<LiveEvent> lines;

  /// Called when a chat line is tapped (the host uses this to mute or kick).
  final void Function(LiveEvent line)? onLineTap;
  final Future<bool> Function(String text) onSend;

  @override
  State<ChatPanel> createState() => _ChatPanelState();
}

class _ChatPanelState extends State<ChatPanel> {
  final _c = TextEditingController();
  bool _sending = false;

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  Future<void> _send() async {
    final text = _c.text.trim();
    if (text.isEmpty || _sending) return;
    setState(() => _sending = true);
    final ok = await widget.onSend(text);
    if (!mounted) return;
    setState(() => _sending = false);
    if (ok) _c.clear();
  }

  @override
  Widget build(BuildContext context) {
    final recent = widget.lines.length > 6
        ? widget.lines.sublist(widget.lines.length - 6)
        : widget.lines;
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (final l in recent)
          InkWell(
            key: ValueKey('chat_line_${l.id}'),
            borderRadius: BorderRadius.circular(16),
            onTap: widget.onLineTap == null ? null : () => widget.onLineTap!(l),
            child: Padding(
              padding: const EdgeInsets.only(bottom: 5),
              // Each line sits on its own soft dark pill so it stays readable
              // over bright video.
              child: DecoratedBox(
                decoration: BoxDecoration(color: Colors.black.withValues(alpha: 0.42), borderRadius: BorderRadius.circular(16)),
                child: Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                  child: Text.rich(
                    TextSpan(
                      children: [
                        TextSpan(
                          text: '${l.name}  ',
                          style: TextStyle(fontWeight: FontWeight.w700, color: _nameColour(l.name)),
                        ),
                        TextSpan(
                          text: l.text,
                          style: const TextStyle(color: Colors.white),
                        ),
                      ],
                    ),
                    key: ValueKey('chat_${l.id}'),
                  ),
                ),
              ),
            ),
          ),
        const SizedBox(height: 4),
        Row(
          children: [
            Expanded(
              child: TextField(
                key: const Key('live_chat_input'),
                controller: _c,
                maxLength: 200,
                style: const TextStyle(color: Colors.white),
                onSubmitted: (_) => _send(),
                decoration: InputDecoration(
                  hintText: 'Say something…',
                  hintStyle: const TextStyle(color: Colors.white70),
                  counterText: '',
                  isDense: true,
                  filled: true,
                  fillColor: Colors.white.withValues(alpha: 0.16),
                  contentPadding: const EdgeInsets.symmetric(horizontal: 18, vertical: 12),
                  border: OutlineInputBorder(borderRadius: BorderRadius.circular(Radii.pill), borderSide: BorderSide.none),
                  enabledBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(Radii.pill), borderSide: BorderSide.none),
                  focusedBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(Radii.pill), borderSide: const BorderSide(color: Colors.white70, width: 1.2)),
                ),
              ),
            ),
            const SizedBox(width: 6),
            IconButton.filled(
              key: const Key('live_chat_send'),
              onPressed: _sending ? null : _send,
              style: IconButton.styleFrom(
                backgroundColor: ModeLight.daylight.start,
                foregroundColor: ModeLight.daylight.ink,
                disabledBackgroundColor: Colors.white24,
                fixedSize: const Size(46, 46),
              ),
              icon: const Icon(Icons.arrow_upward_rounded),
              tooltip: 'Send',
            ),
          ],
        ),
      ],
    );
  }
}
