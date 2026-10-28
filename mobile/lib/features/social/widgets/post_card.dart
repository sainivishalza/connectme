import 'package:flutter/material.dart';

import '../../../core/theme/tokens.dart';
import '../../../shared/widgets/avatar.dart';
import '../social_repository.dart';

/// "5 min ago", "3 h ago", "2 d ago".
String timeAgo(DateTime t, {DateTime? now}) {
  final d = (now ?? DateTime.now()).difference(t);
  if (d.inMinutes < 1) return 'just now';
  if (d.inMinutes < 60) return '${d.inMinutes} min ago';
  if (d.inHours < 24) return '${d.inHours} h ago';
  return '${d.inDays} d ago';
}

/// One post. Moments show their author the way the author's mode allows,
/// Little Universe posts show only a made-up nickname, and missed-connection
/// posts show only the venue.
class PostCard extends StatelessWidget {
  const PostCard({
    super.key,
    required this.post,
    required this.onLike,
    this.onReport,
    this.onDelete,
    this.onClaim,
    this.onOpenClaims,
    this.onComments,
  });

  final Post post;
  final VoidCallback onLike;
  final VoidCallback? onReport;
  final VoidCallback? onDelete;
  final VoidCallback? onClaim;
  final VoidCallback? onOpenClaims;
  final VoidCallback? onComments;

  String get _who {
    if (post.mine) return 'You';
    if (post.kind == 'universe') return post.alias;
    if (post.kind == 'missed') return 'Someone at ${post.hotelName.isEmpty ? 'your venue' : post.hotelName}';
    return post.author?.title ?? 'Anonymous';
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final light = switch (post.kind) {
      'universe' => ModeLight.dusk,
      'missed' => ModeLight.lamplight,
      _ => ModeLight.daylight,
    };
    return Card(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 14, 8, 8),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                if (post.kind == 'moment' && post.author != null)
                  Avatar(
                    name: post.author!.displayName,
                    photoPath: post.author!.photos.isEmpty ? null : post.author!.photos.first,
                    anonymous: post.author!.anonymous,
                    radius: 18,
                    mode: post.author!.mode,
                  )
                else
                  Container(
                    width: 36,
                    height: 36,
                    decoration: BoxDecoration(shape: BoxShape.circle, gradient: light.gradient),
                    child: Icon(post.kind == 'missed' ? Icons.place_outlined : Icons.visibility_off_outlined, size: 18, color: light.ink),
                  ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(_who, style: t.textTheme.titleSmall, overflow: TextOverflow.ellipsis),
                      Text(
                        [timeAgo(post.createdAt), if (post.distance.isNotEmpty) post.distance].join(', '),
                        style: t.textTheme.bodySmall,
                      ),
                    ],
                  ),
                ),
                if (onReport != null || onDelete != null)
                  PopupMenuButton<String>(
                    key: Key('post_menu_${post.id}'),
                    onSelected: (v) => v == 'report' ? onReport?.call() : onDelete?.call(),
                    itemBuilder: (_) => [
                      if (onDelete != null) const PopupMenuItem(value: 'delete', child: Text('Delete')),
                      if (onReport != null) const PopupMenuItem(value: 'report', child: Text('Report')),
                    ],
                  ),
              ],
            ),
            const SizedBox(height: 10),
            Padding(
              padding: const EdgeInsets.only(right: 8),
              child: Text(post.body, style: t.textTheme.bodyLarge),
            ),
            const SizedBox(height: 6),
            Row(
              children: [
                TextButton.icon(
                  key: Key('post_like_${post.id}'),
                  onPressed: onLike,
                  icon: Icon(post.liked ? Icons.favorite : Icons.favorite_border, size: 20, color: post.liked ? ModeLight.daylight.solid : null),
                  label: Text('${post.likes}'),
                ),
                if (onComments != null && post.kind != 'missed')
                  TextButton.icon(
                    key: Key('post_comments_${post.id}'),
                    onPressed: onComments,
                    icon: const Icon(Icons.mode_comment_outlined, size: 20),
                    label: Text('${post.comments}'),
                  ),
                const Spacer(),
                if (post.kind == 'missed' && !post.mine)
                  FilledButton.tonal(
                    key: Key('post_claim_${post.id}'),
                    onPressed: post.claimed ? null : onClaim,
                    style: FilledButton.styleFrom(minimumSize: const Size(0, 40)),
                    child: Text(post.claimed ? 'Sent' : "That's me"),
                  ),
                if (post.kind == 'missed' && post.mine)
                  FilledButton.tonal(
                    key: Key('post_claims_${post.id}'),
                    onPressed: onOpenClaims,
                    style: FilledButton.styleFrom(minimumSize: const Size(0, 40)),
                    child: Text(post.claims == 0 ? 'No replies yet' : '${post.claims} said it was them'),
                  ),
                const SizedBox(width: 8),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
