import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../../features/calls/incoming_call_watcher.dart';
import '../../features/push/push_registrar.dart';
import '../theme/tokens.dart';

/// The five main tabs. Each tab keeps its own navigation stack. The bar is a
/// floating ink pill: it sits on the page surface with a margin around it
/// rather than in a full-width toolbar.
class AppShell extends StatelessWidget {
  const AppShell({super.key, required this.shell});
  final StatefulNavigationShell shell;

  static const _items = [
    (Icons.favorite_border, Icons.favorite, 'Discover'),
    (Icons.hotel_outlined, Icons.hotel, 'Hotel'),
    (Icons.groups_outlined, Icons.groups, 'Groups'),
    (Icons.chat_bubble_outline, Icons.chat_bubble, 'Chats'),
    (Icons.person_outline, Icons.person, 'Profile'),
  ];

  @override
  Widget build(BuildContext context) {
    return PushRegistrar(
      child: IncomingCallWatcher(
      child: Scaffold(
      body: shell,
      bottomNavigationBar: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 0, 20, 12),
          child: _PillNav(
            index: shell.currentIndex,
            onSelect: (i) => shell.goBranch(i, initialLocation: i == shell.currentIndex),
          ),
        ),
      ),
      ),
      ),
    );
  }
}

class _PillNav extends StatelessWidget {
  const _PillNav({required this.index, required this.onSelect});
  final int index;
  final ValueChanged<int> onSelect;

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    final bar = dark ? Palette.nightRaised : Palette.midnight;
    return Container(
      height: 64,
      decoration: BoxDecoration(
        color: bar,
        borderRadius: BorderRadius.circular(Radii.pill),
        border: dark ? Border.all(color: Palette.nightLine) : null,
        boxShadow: [BoxShadow(color: Palette.midnight.withValues(alpha: 0.28), blurRadius: 28, offset: const Offset(0, 10))],
      ),
      padding: const EdgeInsets.all(6),
      child: Row(
        children: [
          for (var i = 0; i < AppShell._items.length; i++)
            Expanded(
              child: Semantics(
                button: true,
                selected: i == index,
                label: AppShell._items[i].$3,
                child: _NavItem(
                  icon: i == index ? AppShell._items[i].$2 : AppShell._items[i].$1,
                  label: AppShell._items[i].$3,
                  selected: i == index,
                  onTap: () => onSelect(i),
                ),
              ),
            ),
        ],
      ),
    );
  }
}

class _NavItem extends StatelessWidget {
  const _NavItem({required this.icon, required this.label, required this.selected, required this.onTap});
  final IconData icon;
  final String label;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return Tooltip(
      message: label,
      child: InkWell(
        borderRadius: BorderRadius.circular(Radii.pill),
        onTap: onTap,
        child: AnimatedContainer(
          duration: const Duration(milliseconds: 220),
          curve: Curves.easeOutCubic,
          decoration: BoxDecoration(
            gradient: selected ? ModeLight.daylight.gradient : null,
            borderRadius: BorderRadius.circular(Radii.pill),
          ),
          alignment: Alignment.center,
          child: Icon(icon, size: 24, color: selected ? ModeLight.daylight.ink : Colors.white.withValues(alpha: 0.78)),
        ),
      ),
    );
  }
}
