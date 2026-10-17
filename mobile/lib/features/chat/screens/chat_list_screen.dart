import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

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
      appBar: AppBar(title: const Text('Chats')),
      body: AsyncListView<Conversation>(
        value: convs,
        onRefresh: () async => ref.refresh(conversationsProvider.future),
        emptyTitle: 'No conversations yet',
        emptyMessage: 'When you match with someone, your chat appears here.',
        emptyIcon: Icons.chat_bubble_outline,
        padding: const EdgeInsets.symmetric(vertical: 8, horizontal: 8),
        itemBuilder: (context, c) => ListTile(
          key: ValueKey(c.id),
          leading: c.isGroup
              ? const CircleAvatar(child: Icon(Icons.groups))
              : Avatar(
                  name: c.title,
                  photoPath: c.photoPath,
                  anonymous: c.anonymous,
                ),
          title: Text(c.title),
          subtitle: Text(
            c.subtitle,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
          ),
          trailing: const Icon(Icons.chevron_right),
          onTap: () =>
              context.push('/chats/${c.id}?group=${c.isGroup ? 1 : 0}'),
        ),
      ),
    );
  }
}
