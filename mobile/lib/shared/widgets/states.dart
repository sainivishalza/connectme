import 'package:flutter/material.dart';
import '../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/network/api_client.dart';
import '../../core/theme/tokens.dart';

/// Centered progress indicator.
class Loader extends StatelessWidget {
  const Loader({super.key});
  @override
  Widget build(BuildContext context) => const Center(child: CircularProgressIndicator());
}

/// Friendly empty state with an optional action.
class EmptyState extends StatelessWidget {
  const EmptyState({super.key, required this.title, this.message, this.icon = Icons.inbox_outlined, this.action});
  final String title;
  final String? message;
  final IconData icon;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            _Emblem(icon: icon),
            const SizedBox(height: 26),
            Text(title, style: t.textTheme.headlineSmall, textAlign: TextAlign.center),
            if (message != null) ...[
              const SizedBox(height: 10),
              Text(message!, style: t.textTheme.bodyMedium?.copyWith(color: t.colorScheme.onSurfaceVariant), textAlign: TextAlign.center),
            ],
            if (action != null) ...[const SizedBox(height: 20), action!],
          ],
        ),
      ),
    );
  }
}

/// The picture above an empty state: two tilted cards in the mode colours fan
/// out behind a gradient disc, so a blank screen still feels like part of the
/// app. It settles in once when the screen opens (not at all with reduced motion).
class _Emblem extends StatelessWidget {
  const _Emblem({required this.icon});
  final IconData icon;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final dark = t.brightness == Brightness.dark;
    final still = MediaQuery.of(context).disableAnimations;
    Widget tile(ModeLight m, double angle, Offset shift) => Transform.translate(
          offset: shift,
          child: Transform.rotate(
            angle: angle,
            child: Container(
              width: 74,
              height: 96,
              decoration: BoxDecoration(
                borderRadius: BorderRadius.circular(Radii.control + 4),
                gradient: m.wash(t.brightness),
                border: Border.all(color: m.start.withValues(alpha: dark ? 0.35 : 0.5), width: 1.2),
              ),
            ),
          ),
        );
    final art = SizedBox(
      width: 168,
      height: 128,
      child: Stack(alignment: Alignment.center, children: [
        tile(ModeLight.dusk, 0.24, const Offset(46, 6)),
        tile(ModeLight.lamplight, -0.24, const Offset(-46, 6)),
        Container(
          width: 94,
          height: 94,
          decoration: BoxDecoration(
            shape: BoxShape.circle,
            gradient: ModeLight.daylight.gradient,
            boxShadow: [BoxShadow(color: ModeLight.daylight.solid.withValues(alpha: 0.35), blurRadius: 28, offset: const Offset(0, 10))],
          ),
          child: Icon(icon, size: 42, color: ModeLight.daylight.ink),
        ),
      ]),
    );
    if (still) return art;
    return TweenAnimationBuilder<double>(
      tween: Tween(begin: 0.86, end: 1),
      duration: const Duration(milliseconds: 520),
      curve: Curves.easeOutBack,
      builder: (c, v, child) => Opacity(opacity: ((v - 0.86) / 0.14).clamp(0.0, 1.0), child: Transform.scale(scale: v, child: child)),
      child: art,
    );
  }
}

/// Error state with retry. Shows the API message when there is one.
class ErrorView extends StatelessWidget {
  const ErrorView({super.key, required this.error, this.onRetry});
  final Object error;
  final VoidCallback? onRetry;

  static String messageOf(Object e) =>
      e is ApiException ? e.message : 'Something went wrong. Please try again.';

  @override
  Widget build(BuildContext context) {
    return EmptyState(
      icon: Icons.error_outline,
      title: 'Could not load',
      message: messageOf(error),
      action: onRetry == null ? null : OutlinedButton.icon(onPressed: onRetry, icon: const Icon(Icons.refresh), label: Text(AppLocalizations.of(context)?.sTryAgain ?? 'Try again')),
    );
  }
}

/// Renders an AsyncValue with loading / error / empty / data states and
/// pull-to-refresh, so every list screen behaves the same way.
class AsyncListView<T> extends StatelessWidget {
  const AsyncListView({
    super.key,
    required this.value,
    required this.onRefresh,
    required this.itemBuilder,
    required this.emptyTitle,
    this.emptyMessage,
    this.emptyIcon = Icons.inbox_outlined,
    this.emptyAction,
    this.padding = const EdgeInsets.all(16),
  });

  final AsyncValue<List<T>> value;
  final Future<void> Function() onRefresh;
  final Widget Function(BuildContext, T) itemBuilder;
  final String emptyTitle;
  final String? emptyMessage;
  final IconData emptyIcon;
  final Widget? emptyAction;
  final EdgeInsets padding;

  @override
  Widget build(BuildContext context) {
    if (!value.hasValue && value.isLoading) return const Loader();
    if (!value.hasValue && value.hasError) {
      return ErrorView(error: value.error!, onRetry: () => onRefresh());
    }
    final items = value.value ?? <T>[];
    return RefreshIndicator(
      onRefresh: onRefresh,
      child: items.isEmpty
          ? LayoutBuilder(
              builder: (context, c) => SingleChildScrollView(
                physics: const AlwaysScrollableScrollPhysics(),
                child: SizedBox(
                  height: c.maxHeight,
                  child: EmptyState(title: emptyTitle, message: emptyMessage, icon: emptyIcon, action: emptyAction),
                ),
              ),
            )
          : ListView.separated(
              physics: const AlwaysScrollableScrollPhysics(),
              padding: padding,
              itemCount: items.length,
              separatorBuilder: (_, _) => const SizedBox(height: 12),
              itemBuilder: (c, i) => itemBuilder(c, items[i]),
            ),
    );
  }
}
