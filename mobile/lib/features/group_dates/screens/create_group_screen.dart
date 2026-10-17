import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_button.dart';
import '../../../shared/widgets/app_snack.dart';
import '../groups_repository.dart';
import '../widgets/group_card.dart';

class CreateGroupScreen extends ConsumerStatefulWidget {
  const CreateGroupScreen({super.key});
  @override
  ConsumerState<CreateGroupScreen> createState() => _CreateGroupScreenState();
}

class _CreateGroupScreenState extends ConsumerState<CreateGroupScreen> {
  final _form = GlobalKey<FormState>();
  final _name = TextEditingController();
  final _label = TextEditingController();
  final _desc = TextEditingController();
  String _type = 'double';
  String _activity = 'dinner';
  double _size = 4;
  bool _busy = false;

  @override
  void dispose() {
    _name.dispose();
    _label.dispose();
    _desc.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (!_form.currentState!.validate()) return;
    setState(() => _busy = true);
    String? newId;
    final ok = await guarded(context, () async {
      final g = await ref.read(groupsRepositoryProvider).create(
            name: _name.text.trim(),
            type: _type,
            activity: _activity,
            activityLabel: _label.text.trim(),
            targetSize: _type == 'group' ? _size.round() : null,
            description: _desc.text.trim(),
          );
      newId = g.id;
      ref.invalidate(myGroupsProvider);
    });
    if (!mounted) return;
    setState(() => _busy = false);
    if (ok && newId != null) context.pushReplacement('/groups/$newId');
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('New group')),
      body: Form(
        key: _form,
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            SegmentedButton<String>(
              key: const Key('group_type'),
              segments: const [
                ButtonSegment(value: 'double', label: Text('Double date (2)'), icon: Icon(Icons.people_alt)),
                ButtonSegment(value: 'group', label: Text('Group (2 to 6)'), icon: Icon(Icons.groups)),
              ],
              selected: {_type},
              onSelectionChanged: (s) => setState(() => _type = s.first),
            ),
            const SizedBox(height: 8),
            Text(_type == 'double'
                ? 'You and one friend meet another pair.'
                : 'Pick how many people are in your crew (including you).'),
            if (_type == 'group') ...[
              Slider(
                key: const Key('group_size'),
                value: _size,
                min: 2,
                max: 6,
                divisions: 4,
                label: '${_size.round()} people',
                onChanged: (v) => setState(() => _size = v),
              ),
              Text('${_size.round()} people', textAlign: TextAlign.center),
            ],
            const SizedBox(height: 16),
            TextFormField(
              key: const Key('group_name'),
              controller: _name,
              maxLength: 100,
              decoration: const InputDecoration(labelText: 'Group name'),
              validator: (v) => (v ?? '').trim().isEmpty ? 'Give your group a name' : null,
            ),
            const SizedBox(height: 8),
            Text('Activity', style: Theme.of(context).textTheme.titleSmall),
            const SizedBox(height: 8),
            ActivityPicker(value: _activity, onChanged: (v) => setState(() => _activity = v ?? 'dinner')),
            if (_activity == 'custom') ...[
              const SizedBox(height: 12),
              TextFormField(
                controller: _label,
                maxLength: 50,
                decoration: const InputDecoration(labelText: 'What are you doing?'),
                validator: (v) => _activity == 'custom' && (v ?? '').trim().isEmpty ? 'Describe the activity' : null,
              ),
            ],
            const SizedBox(height: 8),
            TextFormField(controller: _desc, maxLength: 280, maxLines: 3, decoration: const InputDecoration(labelText: 'About your group (optional)')),
            const SizedBox(height: 16),
            AppButton(key: const Key('create_group'), label: 'Create group', busy: _busy, onPressed: _submit),
          ],
        ),
      ),
    );
  }
}
