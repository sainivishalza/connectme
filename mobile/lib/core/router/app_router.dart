import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../features/auth/auth_controller.dart';
import '../../features/auth/screens/login_screen.dart';
import '../../features/auth/screens/register_screen.dart';
import '../../features/auth/screens/verify_screen.dart';
import '../../features/chat/screens/chat_list_screen.dart';
import '../../features/chat/screens/chat_screen.dart';
import '../../features/discovery/screens/discover_screen.dart';
import '../../features/group_dates/screens/browse_groups_screen.dart';
import '../../features/calls/calls_repository.dart';
import '../../features/calls/screens/call_screen.dart';
import '../../features/calls/screens/incoming_call_screen.dart';
import '../../features/games/screens/quiz_screen.dart';
import '../../features/forums/forums_repository.dart';
import '../../features/forums/screens/board_screen.dart';
import '../../features/forums/screens/forums_screen.dart';
import '../../features/forums/screens/thread_screen.dart';
import '../../features/partners/screens/events_screen.dart';
import '../../features/push/screens/notifications_screen.dart';
import '../../features/videos/screens/video_feed_screen.dart';
import '../../features/audio/audio_repository.dart';
import '../../features/audio/screens/audio_list_screen.dart';
import '../../features/audio/screens/audio_room_screen.dart';
import '../../features/communities/communities_repository.dart';
import '../../features/communities/screens/communities_screen.dart';
import '../../features/explore/explore_screen.dart';
import '../../features/live/live_repository.dart';
import '../../features/social/screens/feed_screen.dart';
import '../../features/social/social_repository.dart';
import '../../features/random_chat/screens/random_screen.dart';
import '../../features/live/screens/broadcast_screen.dart';
import '../../features/live/screens/live_list_screen.dart';
import '../../features/live/screens/live_room_screen.dart';
import '../../features/group_dates/screens/create_group_screen.dart';
import '../../features/group_dates/screens/group_dates_screen.dart';
import '../../features/group_dates/screens/group_detail_screen.dart';
import '../../features/group_dates/screens/groups_screen.dart';
import '../../features/hotel_mode/screens/hotel_screen.dart';
import '../../features/payments/screens/payments_screens.dart';
import '../../features/profile/screens/edit_profile_screen.dart';
import '../../features/profile/screens/profile_screen.dart';
import '../../shared/widgets/states.dart';
import 'app_shell.dart';

const _authPaths = {'/login', '/register', '/verify', '/forgot', '/reset'};

/// Pure redirect rule, separated so it can be unit-tested.
String? redirectFor(AuthStatus status, String location) {
  final isAuthPage = _authPaths.contains(location);
  switch (status) {
    case AuthStatus.unknown:
      return location == '/' ? null : '/';
    case AuthStatus.signedOut:
      return isAuthPage ? null : '/login';
    case AuthStatus.signedIn:
      return (isAuthPage || location == '/') ? '/discover' : null;
  }
}

class _AuthRefresh extends ChangeNotifier {
  void poke() => notifyListeners();
}

final routerProvider = Provider<GoRouter>((ref) {
  final refresh = _AuthRefresh();
  ref.listen(authControllerProvider.select((s) => s.status), (_, _) => refresh.poke());
  ref.onDispose(refresh.dispose);

  return GoRouter(
    initialLocation: '/',
    refreshListenable: refresh,
    redirect: (context, state) => redirectFor(ref.read(authControllerProvider).status, state.uri.path),
    routes: [
      GoRoute(path: '/', builder: (_, _) => const Scaffold(body: Loader())),
      GoRoute(path: '/login', builder: (_, _) => const LoginScreen()),
      GoRoute(path: '/register', builder: (_, _) => const RegisterScreen()),
      GoRoute(path: '/verify', builder: (_, _) => const VerifyScreen()),
      GoRoute(path: '/forgot', builder: (_, _) => const ForgotScreen()),
      GoRoute(path: '/reset', builder: (_, _) => const VerifyScreen(mode: VerifyMode.resetPassword)),
      GoRoute(path: '/random', builder: (_, _) => const RandomScreen()),
      GoRoute(path: '/masked', builder: (_, _) => const RandomScreen(style: 'truth')),
      GoRoute(path: '/explore', builder: (_, _) => const ExploreScreen()),
      GoRoute(path: '/communities', builder: (_, _) => const CommunitiesScreen()),
      GoRoute(
        path: '/community/:id',
        builder: (_, s) => FeedScreen(kind: PostKind.community, community: s.extra! as Community),
      ),
      GoRoute(
        path: '/feed/:kind',
        builder: (_, s) => FeedScreen(
          kind: PostKind.values.firstWhere((k) => k.api == s.pathParameters['kind'], orElse: () => PostKind.moment),
        ),
      ),
      GoRoute(path: '/quiz/:id', builder: (_, s) => QuizScreen(matchId: s.pathParameters['id']!)),
      GoRoute(path: '/call', builder: (_, s) => CallScreen(started: s.extra! as StartedCall)),
      GoRoute(path: '/call/incoming', builder: (_, s) => IncomingCallScreen(call: s.extra! as CallInfo)),
      GoRoute(path: '/forums', builder: (_, _) => const ForumsScreen()),
      GoRoute(path: '/forums/:slug', builder: (_, s) => BoardScreen(board: s.extra! as Board)),
      GoRoute(path: '/thread/:id', builder: (_, s) => ThreadScreen(threadId: s.pathParameters['id']!)),
      GoRoute(path: '/events', builder: (_, _) => const EventsScreen()),
      GoRoute(path: '/settings/notifications', builder: (_, _) => const NotificationsScreen()),
      GoRoute(path: '/videos', builder: (_, _) => const VideoFeedScreen()),
      GoRoute(path: '/audio', builder: (_, _) => const AudioListScreen()),
      GoRoute(
        path: '/audio/:id',
        builder: (_, s) => AudioRoomScreen(roomId: s.pathParameters['id']!, started: s.extra is StartedAudio ? s.extra as StartedAudio : null),
      ),
      GoRoute(path: '/live', builder: (_, _) => const LiveListScreen()),
      GoRoute(path: '/live/broadcast', builder: (_, _) => const BroadcastScreen()),
      GoRoute(
        path: '/live/:id',
        builder: (_, s) => LiveRoomScreen(
          roomId: s.pathParameters['id']!,
          room: s.extra is LiveRoom ? s.extra as LiveRoom : null,
        ),
      ),
      StatefulShellRoute.indexedStack(
        builder: (context, state, shell) => AppShell(shell: shell),
        branches: [
          StatefulShellBranch(routes: [GoRoute(path: '/discover', builder: (_, _) => const DiscoverScreen())]),
          StatefulShellBranch(routes: [GoRoute(path: '/hotel', builder: (_, _) => const HotelScreen())]),
          StatefulShellBranch(routes: [
            GoRoute(
              path: '/groups',
              builder: (_, _) => const GroupsScreen(),
              routes: [
                GoRoute(path: 'new', builder: (_, _) => const CreateGroupScreen()),
                GoRoute(
                  path: ':id',
                  builder: (_, s) => GroupDetailScreen(groupId: s.pathParameters['id']!),
                  routes: [
                    GoRoute(path: 'browse', builder: (_, s) => BrowseGroupsScreen(groupId: s.pathParameters['id']!)),
                    GoRoute(
                      path: 'dates/:gm',
                      builder: (_, s) => GroupDatesScreen(groupId: s.pathParameters['id']!, matchId: s.pathParameters['gm']!),
                    ),
                  ],
                ),
              ],
            ),
          ]),
          StatefulShellBranch(routes: [
            GoRoute(
              path: '/chats',
              builder: (_, _) => const ChatListScreen(),
              routes: [
                GoRoute(
                  path: ':id',
                  builder: (_, s) => ChatScreen(
                    convId: s.pathParameters['id']!,
                    isGroup: s.uri.queryParameters['group'] == '1',
                    openGames: s.uri.queryParameters['game'] == '1',
                  ),
                ),
              ],
            ),
          ]),
          StatefulShellBranch(routes: [
            GoRoute(
              path: '/profile',
              builder: (_, _) => const ProfileScreen(),
              routes: [
                GoRoute(path: 'edit', builder: (_, _) => const EditProfileScreen()),
                GoRoute(path: 'premium', builder: (_, _) => const SubscriptionScreen()),
                GoRoute(path: 'coins', builder: (_, _) => const CoinShopScreen()),
                GoRoute(path: 'gifts', builder: (_, _) => const GiftsScreen()),
              ],
            ),
          ]),
        ],
      ),
    ],
  );
});
