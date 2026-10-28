import 'package:flutter/material.dart';

import '../../../l10n/app_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/theme/tokens.dart';
import '../../../shared/widgets/avatar.dart';
import '../../../shared/widgets/states.dart';
import '../data/chat_models.dart';
import '../data/chat_repository.dart';

class ChatListScreen extends ConsumerWidget {
  const ChatListScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final convs = ref.watch(conversationsProvider);
    return Scaffold(
      appBar: AppBar(title: Text(AppLocalizations.of(context)?.chatsTitle ?? 'Chats'), actions: [
        IconButton(key: const Key('search_chats'), tooltip: 'Search messages', icon: const Icon(Icons.search), onPressed: () => context.push('/search')),
      ]),
      body: AsyncListView<Conversation>(
        value: convs,
        onRefresh: () async => ref.refresh(conversationsProvider.future),
        emptyTitle: 'No conversations yet',
        emptyMessage: 'When you match with someone, your chat appears here.',
        emptyIcon: Icons.chat_bubble_outline,
        padding: const EdgeInsets.fromLTRB(16, 8, 16, 24),
        itemBuilder: (context, c) => _ConversationTile(c: c),
      ),
    );
  }
}

/// One conversation as a soft card: a larger avatar, the name in bold, and the
/// latest line (or an invitation to start talking when there is none yet).
class _ConversationTile extends StatelessWidget {
  const _ConversationTile({required this.c});
  final Conversation c;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final cs = t.colorScheme;
    final preview = c.subtitle.trim().isNotEmpty
        ? c.subtitle
        : c.theirOpeningNote.trim().isNotEmpty
            ? c.theirOpeningNote
            : 'Say hello';
    final started = c.subtitle.trim().isNotEmpty;
    return Material(
      color: cs.surfaceContainerLow,
      borderRadius: BorderRadius.circular(Radii.card),
      clipBehavior: Clip.antiAlias,
      child: ListTile(
        key: ValueKey(c.id),
        contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 6),
        leading: c.isGroup
            ? CircleAvatar(radius: 26, backgroundColor: cs.primaryContainer, child: Icon(Icons.groups, color: cs.onPrimaryContainer))
            : Avatar(name: c.title, photoPath: c.photoPath, anonymous: c.anonymous),
        title: Text(c.title, maxLines: 1, overflow: TextOverflow.ellipsis, style: t.textTheme.titleMedium?.copyWith(fontWeight: FontWeight.w700)),
        subtitle: Padding(
          padding: const EdgeInsets.only(top: 2),
          child: Text(
            preview,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: t.textTheme.bodyMedium?.copyWith(
              color: started ? cs.onSurfaceVariant : cs.primary,
              fontWeight: started ? FontWeight.w400 : FontWeight.w600,
            ),
          ),
        ),
        trailing: Icon(Icons.chevron_right, color: cs.onSurfaceVariant),
        onTap: () => context.push('/chats/${c.id}?group=${c.isGroup ? 1 : 0}'),
      ),
    );
  }
}
