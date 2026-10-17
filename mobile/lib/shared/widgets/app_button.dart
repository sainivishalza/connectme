import 'package:flutter/material.dart';

/// Primary button that shows progress and ignores taps while busy.
class AppButton extends StatelessWidget {
  const AppButton({
    super.key,
    required this.label,
    required this.onPressed,
    this.busy = false,
    this.outlined = false,
    this.icon,
  });

  final String label;
  final VoidCallback? onPressed;
  final bool busy;
  final bool outlined;
  final IconData? icon;

  @override
  Widget build(BuildContext context) {
    final child = busy
        ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))
        : Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (icon != null) ...[Icon(icon, size: 18), const SizedBox(width: 8)],
              Text(label),
            ],
          );
    final cb = busy ? null : onPressed;
    return outlined
        ? OutlinedButton(
            onPressed: cb,
            style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(48)),
            child: child)
        : FilledButton(onPressed: cb, child: child);
  }
}
