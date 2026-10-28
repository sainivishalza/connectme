import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../../features/calls/incoming_call_watcher.dart';
import '../../features/push/push_registrar.dart';
import '../../l10n/app_localizations.dart';
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
    final l = AppLocalizations.of(context);
    final names = [
      l?.navDiscover ?? AppShell._items[0].$3,
      l?.navHotel ?? AppShell._items[1].$3,
      l?.navGroups ?? AppShell._items[2].$3,
      l?.navChats ?? AppShell._items[3].$3,
      l?.navProfile ?? AppShell._items[4].$3,
    ];
    final bar = dark ? Palette.nightRaised : Palette.midnight;
    return Container(
      height: 68,
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
                label: names[i],
                child: _NavItem(
                  icon: i == index ? AppShell._items[i].$2 : AppShell._items[i].$1,
                  label: names[i],
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
          // Icon and word together: an icon alone is a guess. The word is held
          // to a small size so a large system text setting cannot break the bar.
          child: MediaQuery.withClampedTextScaling(
            maxScaleFactor: 1.15,
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                Icon(icon, size: 22, color: selected ? ModeLight.daylight.ink : Colors.white.withValues(alpha: 0.84)),
                const SizedBox(height: 2),
                FittedBox(
                  fit: BoxFit.scaleDown,
                  child: Text(label, maxLines: 1, style: TextStyle(fontSize: 11, height: 1.1, fontWeight: selected ? FontWeight.w700 : FontWeight.w500, color: selected ? ModeLight.daylight.ink : Colors.white.withValues(alpha: 0.84))),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
