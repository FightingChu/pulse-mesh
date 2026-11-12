import 'package:flutter/material.dart';

import 'api.dart';

void main() => runApp(const PulseApp());

class PulseApp extends StatelessWidget {
  const PulseApp({super.key});
  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'PulseMesh 工业巡检',
      theme: ThemeData(
        useMaterial3: true,
        colorScheme: ColorScheme.fromSeed(seedColor: const Color(0xFF0B6E4F)),
      ),
      home: const HomePage(),
    );
  }
}

class HomePage extends StatelessWidget {
  const HomePage({super.key});

  @override
  Widget build(BuildContext context) {
    return DefaultTabController(
      length: 2,
      child: Scaffold(
        appBar: AppBar(
          title: const Text('PulseMesh 工业巡检'),
          bottom: const TabBar(
            tabs: [
              Tab(icon: Icon(Icons.sensors), text: '巡检'),
              Tab(icon: Icon(Icons.notifications_active), text: '告警'),
            ],
          ),
        ),
        body: const TabBarView(
          children: [SamplesView(), AlertsView()],
        ),
      ),
    );
  }
}

Color levelColor(String level) {
  switch (level) {
    case 'critical':
      return const Color(0xFFC62828);
    case 'warning':
      return const Color(0xFFEF6C00);
    case 'normal':
      return const Color(0xFF2E7D32);
    default:
      return const Color(0xFF616161);
  }
}

IconData levelIcon(String level) {
  switch (level) {
    case 'critical':
      return Icons.error;
    case 'warning':
      return Icons.warning_amber;
    case 'normal':
      return Icons.check_circle;
    default:
      return Icons.help_outline;
  }
}

class SamplesView extends StatefulWidget {
  const SamplesView({super.key});

  @override
  State<SamplesView> createState() => _SamplesViewState();
}

class _SamplesViewState extends State<SamplesView> {
  final ApiClient api = ApiClient();
  List<Sample> samples = [];
  String status = 'loading';

  Future<void> load() async {
    try {
      final data = await api.samples();
      if (!mounted) return;
      setState(() {
        samples = data;
        status = 'ok';
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        status = 'offline';
      });
    }
  }

  @override
  void initState() {
    super.initState();
    load();
  }

  @override
  void dispose() {
    api.close();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      floatingActionButton: FloatingActionButton(
        onPressed: load,
        child: const Icon(Icons.refresh),
      ),
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.all(12),
            child: Row(
              children: [
                Icon(
                  status == 'ok' ? Icons.cloud_done : Icons.cloud_off,
                  color: status == 'ok' ? Colors.green : Colors.red,
                  size: 18,
                ),
                const SizedBox(width: 8),
                Text('云端 $status  ·  ${samples.length} 条'),
              ],
            ),
          ),
          Expanded(
            child: ListView.builder(
              itemCount: samples.length,
              itemBuilder: (context, index) {
                final s = samples[index];
                return ListTile(
                  leading: Icon(levelIcon(s.level), color: levelColor(s.level)),
                  title: Text('${s.deviceId} · ${s.metric}'),
                  subtitle: Text(s.ts),
                  trailing: Text(
                    s.value.toStringAsFixed(2),
                    style: TextStyle(
                      fontSize: 18,
                      fontWeight: FontWeight.w600,
                      color: levelColor(s.level),
                    ),
                  ),
                );
              },
            ),
          ),
        ],
      ),
    );
  }
}

class AlertsView extends StatefulWidget {
  const AlertsView({super.key});

  @override
  State<AlertsView> createState() => _AlertsViewState();
}

class _AlertsViewState extends State<AlertsView> {
  final ApiClient api = ApiClient();
  List<AlertItem> alerts = [];
  String status = 'loading';

  Future<void> load() async {
    try {
      final data = await api.alerts(includeAcked: true);
      if (!mounted) return;
      setState(() {
        alerts = data;
        status = 'ok';
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        status = 'offline';
      });
    }
  }

  Future<void> ack(AlertItem item) async {
    try {
      await api.ack(item.id);
      await load();
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('确认失败，检查云端连接')),
      );
    }
  }

  @override
  void initState() {
    super.initState();
    load();
  }

  @override
  void dispose() {
    api.close();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final open = alerts.where((item) => !item.acked).length;
    return Scaffold(
      floatingActionButton: FloatingActionButton(
        onPressed: load,
        child: const Icon(Icons.refresh),
      ),
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.all(12),
            child: Row(
              children: [
                Icon(
                  status == 'ok' ? Icons.cloud_done : Icons.cloud_off,
                  color: status == 'ok' ? Colors.green : Colors.red,
                  size: 18,
                ),
                const SizedBox(width: 8),
                Text('未确认告警 $open 条'),
              ],
            ),
          ),
          Expanded(
            child: alerts.isEmpty
                ? const Center(child: Text('暂无告警'))
                : ListView.separated(
                    itemCount: alerts.length,
                    separatorBuilder: (context, index) => const Divider(height: 1),
                    itemBuilder: (context, index) {
                      final a = alerts[index];
                      return ListTile(
                        leading: Icon(levelIcon(a.level), color: levelColor(a.level)),
                        title: Text(a.message),
                        subtitle: Text('${a.deviceId} · ${a.metric}'),
                        trailing: a.acked
                            ? const Chip(label: Text('已确认'))
                            : IconButton(
                                icon: const Icon(Icons.done_all),
                                tooltip: '确认',
                                onPressed: () => ack(a),
                              ),
                      );
                    },
                  ),
          ),
        ],
      ),
    );
  }
}
