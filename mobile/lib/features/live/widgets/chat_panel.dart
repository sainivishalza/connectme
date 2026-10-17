import 'package:flutter/material.dart';

import '../live_repository.dart';

/// Recent chat lines over the video plus a one-line composer.
class ChatPanel extends StatefulWidget {
  const ChatPanel({super.key, required this.lines, required this.onSend, this.onLineTap});
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
    final recent = widget.lines.length > 6 ? widget.lines.sublist(widget.lines.length - 6) : widget.lines;
    return Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
      for (final l in recent)
        InkWell(
          key: ValueKey('chat_line_${l.id}'),
          onTap: widget.onLineTap == null ? null : () => widget.onLineTap!(l),
          child: Padding(
          padding: const EdgeInsets.only(bottom: 2),
          child: Text.rich(
            TextSpan(children: [
              TextSpan(text: '${l.name}  ', style: const TextStyle(fontWeight: FontWeight.bold, color: Colors.white)),
              TextSpan(text: l.text, style: const TextStyle(color: Colors.white70)),
            ]),
            key: ValueKey('chat_${l.id}'),
          ),
        )),
      const SizedBox(height: 6),
      Row(children: [
        Expanded(
          child: TextField(
            key: const Key('live_chat_input'),
            controller: _c,
            maxLength: 200,
            style: const TextStyle(color: Colors.white),
            onSubmitted: (_) => _send(),
            decoration: const InputDecoration(
              hintText: 'Say something…',
              hintStyle: TextStyle(color: Colors.white54),
              counterText: '',
              isDense: true,
              filled: true,
              fillColor: Colors.black45,
              border: OutlineInputBorder(borderSide: BorderSide.none),
            ),
          ),
        ),
        IconButton(
          key: const Key('live_chat_send'),
          onPressed: _sending ? null : _send,
          icon: const Icon(Icons.send, color: Colors.white),
          tooltip: 'Send',
        ),
      ]),
    ]);
  }
}
