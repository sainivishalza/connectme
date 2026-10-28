import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/semantics.dart' show CustomSemanticsAction;
import 'package:flutter/services.dart';

/// How far the top card has been dragged towards each action (0 to 1).
class SwipeProgress {
  const SwipeProgress({this.like = 0, this.nope = 0, this.superLike = 0});
  final double like;
  final double nope;
  final double superLike;
}

/// Lets buttons outside the stack throw the top card.
class SwipeStackController {
  _SwipeStackState? _state;

  /// Animates the top card away as [action] ('pass', 'like' or 'superlike').
  Future<void> swipe(String action) async => _state?._fling(action);
}

/// A stack of cards where the top one can be dragged: right to like, left to
/// pass, up to super like. If [onSwipe] returns false (for example the server
/// refused) the card springs back. Arrow keys work too, for the web build.
class SwipeStack<T> extends StatefulWidget {
  const SwipeStack({
    super.key,
    required this.items,
    required this.keyOf,
    required this.cardBuilder,
    required this.onSwipe,
    this.labelOf,
    this.controller,
    this.enabled = true,
  });

  /// The deck, top card first.
  final List<T> items;
  final Object Function(T) keyOf;
  final Widget Function(BuildContext context, T item, SwipeProgress progress) cardBuilder;
  final Future<bool> Function(T item, String action) onSwipe;
  final SwipeStackController? controller;
  final bool enabled;

  /// What a screen reader says for the top card; its swipes become actions
  /// (Like, Pass, Super Like) because dragging is not possible without sight.
  final String Function(T item)? labelOf;

  @override
  State<SwipeStack<T>> createState() => _SwipeStackState<T>();
}

class _SwipeStackState<T> extends State<SwipeStack<T>> with SingleTickerProviderStateMixin {
  late final AnimationController _anim = AnimationController(vsync: this, duration: const Duration(milliseconds: 240));
  Offset _pos = Offset.zero;
  Offset _from = Offset.zero;
  Offset _to = Offset.zero;
  bool _busy = false;
  Size _size = Size.zero;

  @override
  void initState() {
    super.initState();
    widget.controller?._state = this;
    _anim.addListener(() {
      final t = Curves.easeOut.transform(_anim.value);
      setState(() => _pos = Offset.lerp(_from, _to, t)!);
    });
  }

  @override
  void didUpdateWidget(SwipeStack<T> old) {
    super.didUpdateWidget(old);
    widget.controller?._state = this;
  }

  @override
  void dispose() {
    if (widget.controller?._state == this) widget.controller!._state = null;
    _anim.dispose();
    super.dispose();
  }

  SwipeProgress get _progress {
    final w = _size.width, h = _size.height;
    if (w == 0 || h == 0) return const SwipeProgress();
    final up = (-_pos.dy / (h * 0.22)).clamp(0.0, 1.0);
    if (up > 0.15 && -_pos.dy > _pos.dx.abs()) return SwipeProgress(superLike: up);
    final side = (_pos.dx.abs() / (w * 0.3)).clamp(0.0, 1.0);
    return _pos.dx >= 0 ? SwipeProgress(like: side) : SwipeProgress(nope: side);
  }

  Future<void> _animateTo(Offset target, {int ms = 240}) {
    _from = _pos;
    _to = target;
    // With reduced motion the card still moves, in a single frame.
    _anim.duration = Duration(milliseconds: MediaQuery.disableAnimationsOf(context) ? 1 : ms);
    return _anim.forward(from: 0).orCancel.catchError((_) {});
  }

  Future<void> _fling(String action) async {
    if (_busy || !widget.enabled || widget.items.isEmpty) return;
    final item = widget.items.first;
    final w = _size.width == 0 ? 400.0 : _size.width;
    final h = _size.height == 0 ? 600.0 : _size.height;
    final target = switch (action) {
      'like' => Offset(w * 1.4, 0),
      'pass' => Offset(-w * 1.4, 0),
      _ => Offset(0, -h * 1.1),
    };
    _busy = true;
    await _animateTo(target);
    final ok = await widget.onSwipe(item, action);
    if (!mounted) return;
    if (ok) {
      setState(() => _pos = Offset.zero); // the next card is now on top
    } else {
      await _animateTo(Offset.zero, ms: 220);
    }
    _busy = false;
  }

  void _end(DragEndDetails d) {
    if (_busy || !widget.enabled) return;
    final p = _progress;
    final v = d.velocity.pixelsPerSecond;
    if (p.superLike >= 1 || v.dy < -1100) {
      _fling('superlike');
    } else if (p.like >= 1 || v.dx > 900) {
      _fling('like');
    } else if (p.nope >= 1 || v.dx < -900) {
      _fling('pass');
    } else {
      _busy = true;
      _animateTo(Offset.zero, ms: 200).then((_) => _busy = false);
    }
  }

  KeyEventResult _key(FocusNode node, KeyEvent e) {
    if (e is! KeyDownEvent) return KeyEventResult.ignored;
    final action = switch (e.logicalKey) {
      LogicalKeyboardKey.arrowRight => 'like',
      LogicalKeyboardKey.arrowLeft => 'pass',
      LogicalKeyboardKey.arrowUp => 'superlike',
      _ => null,
    };
    if (action == null) return KeyEventResult.ignored;
    _fling(action);
    return KeyEventResult.handled;
  }

  @override
  Widget build(BuildContext context) {
    final items = widget.items;
    return Focus(
      autofocus: true,
      onKeyEvent: _key,
      child: LayoutBuilder(builder: (context, box) {
        _size = Size(box.maxWidth, box.maxHeight);
        if (items.isEmpty) return const SizedBox.shrink();
        final p = _progress;
        final drag = math.min(1.0, math.max(p.like, math.max(p.nope, p.superLike)));
        final cards = <Widget>[];
        // Cards behind the top one: slightly smaller, rising as the top card leaves.
        for (var i = math.min(2, items.length - 1); i >= 1; i--) {
          final scale = 1 - 0.05 * i + 0.05 * drag * (i == 1 ? 1 : 0);
          cards.add(Positioned.fill(
            key: ValueKey('behind_${widget.keyOf(items[i])}'),
            child: Transform.translate(
              offset: Offset(0, 14.0 * i - (i == 1 ? 14 * drag : 0)),
              child: Transform.scale(scale: scale, child: IgnorePointer(child: widget.cardBuilder(context, items[i], const SwipeProgress()))),
            ),
          ));
        }
        final angle = (_pos.dx / math.max(1, _size.width)) * 0.35;
        cards.add(Positioned.fill(
          key: ValueKey('top_${widget.keyOf(items.first)}'),
          child: Semantics(
            container: true,
            label: widget.labelOf?.call(items.first),
            customSemanticsActions: widget.enabled
                ? {
                    const CustomSemanticsAction(label: 'Like'): () => _fling('like'),
                    const CustomSemanticsAction(label: 'Pass'): () => _fling('pass'),
                    const CustomSemanticsAction(label: 'Super Like'): () => _fling('superlike'),
                  }
                : const {},
            child: GestureDetector(
              onPanUpdate: (_busy || !widget.enabled) ? null : (d) => setState(() => _pos += d.delta),
              onPanEnd: _end,
              child: Transform.translate(
                offset: _pos,
                child: Transform.rotate(angle: angle, child: widget.cardBuilder(context, items.first, p)),
              ),
            ),
          ),
        ));
        return Stack(children: cards);
      }),
    );
  }
}
