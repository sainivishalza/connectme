import 'package:flutter/material.dart';
import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/network/api_client.dart';
import '../../../shared/widgets/app_snack.dart';
import '../../../shared/widgets/avatar.dart';
import '../../../shared/widgets/states.dart';
import '../../chat/data/chat_repository.dart';
import '../../communities/communities_repository.dart';
import '../../sponsors/sponsored_card.dart';
import '../social_repository.dart';
import '../widgets/comments_sheet.dart';
import '../widgets/post_card.dart';

/// A feed of one kind of post: Moments, Little Universe or Missed connections.
class FeedScreen extends ConsumerStatefulWidget {
  const FeedScreen({super.key, required this.kind, this.community});
  final PostKind kind;

  /// Set to show one community's posts (kind is then PostKind.community).
  final Community? community;

  @override
  ConsumerState<FeedScreen> createState() => _FeedScreenState();
}

class _FeedScreenState extends ConsumerState<FeedScreen> {
  late final SocialRepository _repo;
  late final CommunitiesRepository _comm;
  final _scroll = ScrollController();
  List<Post> _posts = [];
  Object? _error;
  bool _loading = true;
  bool _more = true;
  bool _loadingMore = false;

  PostKind get kind => widget.kind;
  bool get _showsAds => widget.community == null && (kind == PostKind.moment || kind == PostKind.universe);

  @override
  void initState() {
    super.initState();
    _repo = ref.read(socialRepositoryProvider);
    _comm = ref.read(communitiesRepositoryProvider);
    _scroll.addListener(() {
      if (_scroll.position.extentAfter < 300) _loadMore();
    });
    _load();
  }

  @override
  void dispose() {
    _scroll.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final ps = widget.community != null ? await _comm.posts(widget.community!.id) : await _repo.feed(kind);
      if (!mounted) return;
      setState(() {
        _posts = ps;
        _more = ps.length >= 30;
        _error = null;
        _loading = false;
      });
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = e;
          _loading = false;
        });
      }
    }
  }

  Future<void> _loadMore() async {
    if (_loadingMore || !_more || _posts.isEmpty) return;
    _loadingMore = true;
    try {
      final ps = widget.community != null
          ? await _comm.posts(widget.community!.id, before: _posts.last.id)
          : await _repo.feed(kind, before: _posts.last.id);
      if (mounted) {
        setState(() {
          _posts = [..._posts, ...ps];
          _more = ps.length >= 30;
        });
      }
    } catch (_) {
      // transient; scrolling again retries
    } finally {
      _loadingMore = false;
    }
  }

  void _replace(Post p) => setState(() => _posts = [for (final x in _posts) x.id == p.id ? p : x]);

  Future<void> _like(Post p) async {
    try {
      final r = await _repo.like(p.id);
      _replace(p.copyWith(liked: r.liked, likes: r.likes));
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  Future<void> _report(Post p) async {
    const reasons = {'spam': 'Spam', 'harassment': 'Harassment', 'inappropriate_content': 'Inappropriate', 'scam': 'Scam', 'underage': 'Seems underage', 'other': 'Something else'};
    final reason = await showModalBottomSheet<String>(
      context: context, useRootNavigator: true,
      isScrollControlled: true,
      builder: (c) => SafeArea(
        child: SingleChildScrollView(
          child: Column(mainAxisSize: MainAxisSize.min, children: [
            const ListTile(title: Text('Report this post')),
            for (final e in reasons.entries) ListTile(key: Key('report_${e.key}'), title: Text(e.value), onTap: () => Navigator.pop(c, e.key)),
          ]),
        ),
      ),
    );
    if (reason == null || !mounted) return;
    final ok = await guarded(context, () => _repo.report(p.id, reason));
    if (ok && mounted) showMessage(context, 'Thanks. Our team will review it.');
  }

  Future<void> _delete(Post p) async {
    final ok = await guarded(context, () => _repo.delete(p.id));
    if (ok) setState(() => _posts = _posts.where((x) => x.id != p.id).toList());
  }

  Future<void> _claim(Post p) async {
    final ok = await guarded(context, () => _repo.claim(p.id));
    if (ok) {
      _replace(p.copyWith(claimed: true));
      if (mounted) showMessage(context, 'Sent. If they were looking for you, you will match.');
    }
  }

  Future<void> _openClaims(Post p) async {
    await showModalBottomSheet<void>(
      context: context, useRootNavigator: true,
      isScrollControlled: true,
      builder: (c) => _ClaimsSheet(post: p, repo: _repo, onMatch: (matchId) {
        Navigator.pop(c);
        ref.invalidate(conversationsProvider);
        context.push('/chats/$matchId');
      }),
    );
    _load();
  }

  Future<void> _openComments(Post p) async {
    await showModalBottomSheet<void>(
      context: context, useRootNavigator: true,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (c) => CommentsSheet(
        load: () => _repo.comments(p.id),
        send: (body) => _repo.comment(p.id, body),
        delete: _repo.deleteComment,
        report: (id) => _repo.reportComment(id, 'inappropriate_content'),
        canModerate: p.mine,
        onCount: (n) {
          final i = _posts.indexWhere((x) => x.id == p.id);
          if (i >= 0 && _posts[i].comments != n && mounted) setState(() => _posts[i] = _posts[i].copyWith(comments: n));
        },
      ),
    );
  }

  Future<void> _compose() async {
    final body = await showModalBottomSheet<String>(
      context: context, useRootNavigator: true,
      isScrollControlled: true,
      builder: (c) => _ComposeSheet(kind: kind),
    );
    if (body == null || body.isEmpty || !mounted) return;
    final ok = await guarded(context, () => widget.community != null ? _comm.post(widget.community!.id, body) : _repo.create(kind, body));
    if (ok) _load();
  }

  Widget _empty() {
    final (title, msg, icon) = switch (kind) {
      PostKind.moment => ('Nothing nearby yet', 'Be the first to share what is happening around you.', Icons.place_outlined),
      PostKind.universe => ('Quiet in the universe', 'Post something anonymously. Nobody will know it is you.', Icons.nights_stay_outlined),
      PostKind.community => ('No posts yet', 'Say hello to the group.', Icons.chat_bubble_outline),
      PostKind.missed => ('No missed connections', 'Only guests checked in at your venue can see these. Check in, then write one if you want to find someone.', Icons.hotel_outlined),
    };
    return EmptyState(title: title, message: msg, icon: icon);
  }

  Widget _failed() {
    final e = _error;
    if (e is ApiException && e.status == 422) {
      return EmptyState(
        icon: kind == PostKind.missed ? Icons.hotel_outlined : Icons.my_location,
        title: kind == PostKind.missed ? 'Check in at a venue first' : 'Share your location first',
        message: e.message,
        action: OutlinedButton(onPressed: () => context.push(kind == PostKind.missed ? '/hotel' : '/profile/edit'), child: Text(kind == PostKind.missed ? 'Open Hotel mode' : 'Update my location')),
      );
    }
    return ErrorView(error: e!, onRetry: () {
      setState(() => _loading = true);
      _load();
    });
  }

  @override
  Widget build(BuildContext context) {
    Widget body;
    if (_loading) {
      body = const Loader();
    } else if (_error != null) {
      body = _failed();
    } else if (_posts.isEmpty) {
      body = RefreshIndicator(onRefresh: _load, child: ListView(children: [SizedBox(height: 420, child: _empty())]));
    } else {
      body = RefreshIndicator(
        onRefresh: _load,
        child: ListView.separated(
          controller: _scroll,
          physics: const AlwaysScrollableScrollPhysics(),
          padding: const EdgeInsets.fromLTRB(16, 4, 16, 96),
          itemCount: _posts.length + (_showsAds ? 1 : 0),
          separatorBuilder: (_, _) => const SizedBox(height: 12),
          itemBuilder: (c, i) {
            if (_showsAds && i == 0) return const SponsoredSlot();
            final p = _posts[_showsAds ? i - 1 : i];
            return PostCard(
              key: ValueKey(p.id),
              post: p,
              onLike: () => _like(p),
              onReport: p.mine ? null : () => _report(p),
              onDelete: (p.mine || (widget.community?.owner ?? false)) ? () => _delete(p) : null,
              onClaim: () => _claim(p),
              onOpenClaims: () => _openClaims(p),
              onComments: () => _openComments(p),
            );
          },
        ),
      );
    }
    return Scaffold(
      appBar: AppBar(title: Text(widget.community?.name ?? kind.title)),
      floatingActionButton: FloatingActionButton.extended(
        key: const Key('compose_post'),
        onPressed: _compose,
        icon: const Icon(Icons.edit_outlined),
        label: Text(AppLocalizations.of(context)?.sPost ?? 'Post'),
      ),
      body: body,
    );
  }
}

class _ClaimsSheet extends StatefulWidget {
  const _ClaimsSheet({required this.post, required this.repo, required this.onMatch});
  final Post post;
  final SocialRepository repo;
  final void Function(String matchId) onMatch;

  @override
  State<_ClaimsSheet> createState() => _ClaimsSheetState();
}

class _ClaimsSheetState extends State<_ClaimsSheet> {
  List<Claim>? _claims;
  Object? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final c = await widget.repo.claims(widget.post.id);
      if (mounted) setState(() => _claims = c);
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _accept(Claim c) async {
    try {
      widget.onMatch(await widget.repo.accept(c.id));
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  Future<void> _decline(Claim c) async {
    final ok = await guarded(context, () => widget.repo.decline(c.id));
    if (ok) setState(() => _claims = _claims!.where((x) => x.id != c.id).toList());
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    Widget body;
    if (_error != null) {
      body = ErrorView(error: _error!, onRetry: _load);
    } else if (_claims == null) {
      body = const Padding(padding: EdgeInsets.all(40), child: Loader());
    } else if (_claims!.isEmpty) {
      body = const Padding(padding: EdgeInsets.all(32), child: Text('Nobody has said it was them yet.'));
    } else {
      body = Column(mainAxisSize: MainAxisSize.min, children: [
        for (final c in _claims!)
          ListTile(
            key: Key('claim_${c.id}'),
            leading: Avatar(name: c.claimant.displayName, photoPath: c.claimant.photos.isEmpty ? null : c.claimant.photos.first, anonymous: c.claimant.anonymous, radius: 22, mode: c.claimant.mode),
            title: Text(c.claimant.title),
            subtitle: Text(c.claimant.intro.isEmpty ? (c.claimant.anonymous ? 'Anonymous guest' : '') : c.claimant.intro),
            trailing: Row(mainAxisSize: MainAxisSize.min, children: [
              IconButton(key: Key('claim_decline_${c.id}'), tooltip: 'Not them', icon: const Icon(Icons.close), onPressed: () => _decline(c)),
              IconButton.filled(key: Key('claim_accept_${c.id}'), tooltip: 'It is them', icon: const Icon(Icons.check), onPressed: () => _accept(c)),
            ]),
          ),
      ]);
    }
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.only(bottom: 16),
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 16, 20, 8),
            child: Align(alignment: Alignment.centerLeft, child: Text('Who said it was them', style: t.textTheme.headlineSmall)),
          ),
          body,
        ]),
      ),
    );
  }
}

/// The compose sheet owns its text controller, so it is disposed only after
/// the sheet has finished closing.
class _ComposeSheet extends StatefulWidget {
  const _ComposeSheet({required this.kind});
  final PostKind kind;

  @override
  State<_ComposeSheet> createState() => _ComposeSheetState();
}

class _ComposeSheetState extends State<_ComposeSheet> {
  final _text = TextEditingController();

  @override
  void dispose() {
    _text.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return SafeArea(
      child: SingleChildScrollView(
        padding: EdgeInsets.fromLTRB(20, 4, 20, 20 + MediaQuery.viewInsetsOf(context).bottom),
        child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          Text(switch (widget.kind) {
            PostKind.moment => 'Share a moment',
            PostKind.universe => 'Post anonymously',
            PostKind.missed => 'Write a missed connection',
            PostKind.community => 'Post to the group',
          }, style: t.textTheme.headlineSmall),
          const SizedBox(height: 6),
          Text(switch (widget.kind) {
            PostKind.moment => 'People nearby can see this for a day. Your name shows only if you are in Open mode.',
            PostKind.universe => 'Nobody can tell it is you: you get a made-up nickname. It disappears after two days.',
            PostKind.missed => 'Only people checked in at your venue can see this. Describe the moment, not the person: no names or room numbers.',
            PostKind.community => 'Only members of this group can see it.',
          }, style: t.textTheme.bodyMedium),
          const SizedBox(height: 14),
          TextField(
            key: const Key('post_body'),
            controller: _text,
            autofocus: true,
            maxLength: 280,
            minLines: 3,
            maxLines: 6,
            decoration: const InputDecoration(hintText: 'Say something kind'),
          ),
          const SizedBox(height: 8),
          FilledButton(key: const Key('post_send'), onPressed: () => Navigator.pop(context, _text.text.trim()), child: Text(AppLocalizations.of(context)?.sPost ?? 'Post')),
        ]),
      ),
    );
  }
}
