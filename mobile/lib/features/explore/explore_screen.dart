import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../../core/theme/tokens.dart';

/// Everything beyond swiping, in one place.
class ExploreScreen extends StatelessWidget {
  const ExploreScreen({super.key});

  static const _tiles = <_Tile>[
    _Tile('moments', 'Moments', 'What is happening around you, for a day.', Icons.place_outlined, '/feed/moment', 'open'),
    _Tile('universe', 'Little Universe', 'Anonymous posts from people nearby.', Icons.nights_stay_outlined, '/feed/universe', 'incognito'),
    _Tile('missed', 'Missed connections', 'Find someone you saw at your venue.', Icons.hotel_outlined, '/feed/missed', 'hotel'),
    _Tile('groups', 'Nearby groups', 'Join interest groups near you, or your hotel\'s guests.', Icons.groups_2_outlined, '/communities', 'open'),
    _Tile('videos', 'Short videos', 'Swipe through clips from people nearby in Open mode.', Icons.smart_display_outlined, '/videos', 'open'),
    _Tile('audio', 'Audio rooms', 'Listen in, or raise a hand and join the conversation.', Icons.graphic_eq, '/audio', 'open'),
    _Tile('random', 'Random chat', 'Talk to someone new, anonymously.', Icons.shuffle, '/random', 'incognito'),
    _Tile('masked', 'Masked Truth', 'Anonymous truth or dare with a stranger.', Icons.theater_comedy_outlined, '/masked', 'incognito'),
    _Tile('forums', 'Forums', 'Dating advice, travel tips and city boards.', Icons.forum_outlined, '/forums', 'open'),
    _Tile('events', 'Events and places', 'Hotel mixers, concerts, and venues you can book.', Icons.celebration_outlined, '/events', 'hotel'),
    _Tile('live', 'Live', 'Watch or start a live stream.', Icons.live_tv_outlined, '/live', 'open'),
  ];

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: const Text('Explore')),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(16, 0, 16, 24),
        children: [
          for (final tile in _tiles)
            Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: Card(
                clipBehavior: Clip.antiAlias,
                child: InkWell(
                  key: Key('explore_${tile.id}'),
                  onTap: () => context.push(tile.route),
                  child: Padding(
                    padding: const EdgeInsets.all(16),
                    child: Row(
                      children: [
                        Container(
                          width: 52,
                          height: 52,
                          decoration: BoxDecoration(
                            gradient: ModeLight.of(tile.mode).gradient,
                            borderRadius: BorderRadius.circular(Radii.control),
                          ),
                          child: Icon(tile.icon, color: ModeLight.of(tile.mode).ink),
                        ),
                        const SizedBox(width: 16),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(tile.title, style: t.textTheme.titleMedium),
                              const SizedBox(height: 2),
                              Text(tile.subtitle, style: t.textTheme.bodySmall),
                            ],
                          ),
                        ),
                        const Icon(Icons.chevron_right),
                      ],
                    ),
                  ),
                ),
              ),
            ),
        ],
      ),
    );
  }
}

class _Tile {
  const _Tile(this.id, this.title, this.subtitle, this.icon, this.route, this.mode);
  final String id;
  final String title;
  final String subtitle;
  final IconData icon;
  final String route;
  final String mode;
}
