import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/models/json.dart';
import '../../../core/network/api_client.dart';
import '../../../core/providers.dart';

class GifItem {
  const GifItem({required this.id, required this.title});
  final String id;
  final String title;
}

/// GIF search goes through our server so the GIF service never sees who is
/// searching, and the chosen GIF is sent as an ordinary encrypted attachment.
class GifsRepository {
  GifsRepository(this._api);
  final ApiClient _api;

  Future<List<GifItem>> search(String q) async {
    final m = await _api.getMap('/gifs/search', query: {if (q.trim().isNotEmpty) 'q': q.trim(), 'limit': 24});
    return [for (final j in jsonMaps(m['items'])) GifItem(id: jsonString(j['id']), title: jsonString(j['title']))];
  }

  Future<Uint8List> bytes(String id) => _api.getBytes('/gifs/${Uri.encodeComponent(id)}');
}

final gifsRepositoryProvider = Provider<GifsRepository>((ref) => GifsRepository(ref.watch(apiClientProvider)));

final gifBytesProvider = FutureProvider.autoDispose.family<Uint8List, String>((ref, id) => ref.watch(gifsRepositoryProvider).bytes(id));

/// A sheet to find a GIF; returns its bytes when one is chosen.
Future<Uint8List?> showGifPicker(BuildContext context) {
  return showModalBottomSheet<Uint8List>(
    context: context,
    useRootNavigator: true,
    isScrollControlled: true,
    showDragHandle: true,
    builder: (c) => const _GifSheet(),
  );
}

class _GifSheet extends ConsumerStatefulWidget {
  const _GifSheet();
  @override
  ConsumerState<_GifSheet> createState() => _GifSheetState();
}

class _GifSheetState extends ConsumerState<_GifSheet> {
  final _q = TextEditingController();
  Timer? _debounce;
  List<GifItem>? _items;
  Object? _error;
  bool _sending = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _q.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final r = await ref.read(gifsRepositoryProvider).search(_q.text);
      if (mounted) {
        setState(() {
          _items = r;
          _error = null;
        });
      }
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _choose(GifItem g) async {
    if (_sending) return;
    setState(() => _sending = true);
    try {
      final bytes = await ref.read(gifBytesProvider(g.id).future);
      if (mounted) Navigator.pop(context, bytes);
    } catch (e) {
      if (mounted) {
        setState(() {
          _sending = false;
          _error = e;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final h = MediaQuery.sizeOf(context).height;
    return Padding(
      padding: EdgeInsets.only(bottom: MediaQuery.viewInsetsOf(context).bottom),
      child: SizedBox(
        height: h * 0.6,
        child: Column(children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
            child: TextField(
              key: const Key('gif_search'),
              controller: _q,
              textInputAction: TextInputAction.search,
              decoration: const InputDecoration(prefixIcon: Icon(Icons.search), hintText: 'Search GIFs'),
              onChanged: (_) {
                _debounce?.cancel();
                _debounce = Timer(const Duration(milliseconds: 350), _load);
              },
            ),
          ),
          Expanded(
            child: _error != null
                ? Center(child: Padding(padding: const EdgeInsets.all(24), child: Text(_error is ApiException ? (_error as ApiException).message : 'GIFs are not available right now.', textAlign: TextAlign.center)))
                : _items == null
                    ? const Center(child: CircularProgressIndicator())
                    : _items!.isEmpty
                        ? Center(child: Text('No GIFs found', key: const Key('no_gifs'), style: t.textTheme.titleMedium))
                        : GridView.builder(
                            padding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
                            gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(crossAxisCount: 3, mainAxisSpacing: 8, crossAxisSpacing: 8),
                            itemCount: _items!.length,
                            itemBuilder: (c, i) {
                              final g = _items![i];
                              return InkWell(
                                key: Key('gif_${g.id}'),
                                borderRadius: BorderRadius.circular(12),
                                onTap: () => _choose(g),
                                child: ClipRRect(
                                  borderRadius: BorderRadius.circular(12),
                                  child: ref.watch(gifBytesProvider(g.id)).when(
                                        data: (b) => Image.memory(b, fit: BoxFit.cover, gaplessPlayback: true, semanticLabel: g.title),
                                        loading: () => const ColoredBox(color: Color(0x11000000)),
                                        error: (_, _) => const Icon(Icons.broken_image_outlined),
                                      ),
                                ),
                              );
                            },
                          ),
          ),
          if (_sending) const LinearProgressIndicator(),
        ]),
      ),
    );
  }
}
