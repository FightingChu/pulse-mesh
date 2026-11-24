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
      length: 3,
      child: Scaffold(
        appBar: AppBar(
          title: const Text('PulseMesh 工业巡检'),
          bottom: const TabBar(
            tabs: [
              Tab(icon: Icon(Icons.sensors), text: '巡检'),
              Tab(icon: Icon(Icons.notifications_active), text: '告警'),
              Tab(icon: Icon(Icons.query_stats), text: '诊断'),
            ],
          ),
        ),
        body: const TabBarView(
          children: [SamplesView(), AlertsView(), DiagnosesView()],
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

String _statusForError(Object error) {
  if (error is ApiException) return 'API ${error.statusCode}';
  return '离线';
}

class SamplesView extends StatefulWidget {
  const SamplesView({super.key});

  @override
  State<SamplesView> createState() => _SamplesViewState();
}

class _SamplesViewState extends State<SamplesView> {
  final ApiClient api = ApiClient();
  List<Sample> samples = [];
  String status = '连接中';
  bool loading = false;

  Future<void> load() async {
    if (loading) return;
    setState(() => loading = true);
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
        status = _statusForError(e);
      });
    } finally {
      if (mounted) setState(() => loading = false);
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
        onPressed: loading ? null : load,
        child: loading
            ? const SizedBox(
                width: 22,
                height: 22,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            : const Icon(Icons.refresh),
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
            child: RefreshIndicator(
              onRefresh: load,
              child: samples.isEmpty
                  ? ListView(
                      physics: const AlwaysScrollableScrollPhysics(),
                      children: [
                        SizedBox(
                          height: 240,
                          child: Center(child: Text(loading ? '正在加载' : '暂无巡检数据')),
                        ),
                      ],
                    )
                  : ListView.builder(
                      physics: const AlwaysScrollableScrollPhysics(),
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
  final Set<String> acknowledging = {};
  String status = '连接中';
  bool loading = false;

  Future<void> load() async {
    if (loading) return;
    setState(() => loading = true);
    try {
      final data = await api.alerts(includeAcked: true);
      if (!mounted) return;
      setState(() {
        final locallyAcked = alerts
            .where((alert) => alert.acked)
            .map((alert) => alert.id)
            .toSet();
        alerts = data
            .map((alert) => locallyAcked.contains(alert.id)
                ? alert.copyWith(acked: true)
                : alert)
            .toList();
        status = 'ok';
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        status = _statusForError(e);
      });
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  Future<void> ack(AlertItem item) async {
    if (acknowledging.contains(item.id)) return;
    setState(() => acknowledging.add(item.id));
    try {
      await api.ack(item.id);
      if (!mounted) return;
      setState(() {
        alerts = alerts
            .map((current) => current.id == item.id ? current.copyWith(acked: true) : current)
            .toList();
      });
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('确认失败，检查云端连接')),
      );
    } finally {
      if (mounted) setState(() => acknowledging.remove(item.id));
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
        onPressed: loading ? null : load,
        child: loading
            ? const SizedBox(
                width: 22,
                height: 22,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            : const Icon(Icons.refresh),
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
            child: RefreshIndicator(
              onRefresh: load,
              child: alerts.isEmpty
                  ? ListView(
                      physics: const AlwaysScrollableScrollPhysics(),
                      children: [
                        SizedBox(
                          height: 240,
                          child: Center(child: Text(loading ? '正在加载' : '暂无告警')),
                        ),
                      ],
                    )
                  : ListView.separated(
                      physics: const AlwaysScrollableScrollPhysics(),
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
                              : acknowledging.contains(a.id)
                                  ? const SizedBox(
                                      width: 24,
                                      height: 24,
                                      child: CircularProgressIndicator(strokeWidth: 2),
                                    )
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

class DiagnosesView extends StatefulWidget {
  const DiagnosesView({super.key});

  @override
  State<DiagnosesView> createState() => _DiagnosesViewState();
}

class _DiagnosesViewState extends State<DiagnosesView> {
  final ApiClient api = ApiClient();
  List<Diagnosis> diagnoses = [];
  String status = '连接中';
  bool loading = false;

  Future<void> load() async {
    if (loading) return;
    setState(() => loading = true);
    try {
      final data = await api.diagnoses(limit: 50);
      if (!mounted) return;
      setState(() {
        diagnoses = data;
        status = '在线';
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => status = _statusForError(error));
    } finally {
      if (mounted) setState(() => loading = false);
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
        onPressed: loading ? null : load,
        child: loading
            ? const SizedBox(
                width: 22,
                height: 22,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            : const Icon(Icons.refresh),
      ),
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.all(12),
            child: Row(
              children: [
                Icon(
                  status == '在线' ? Icons.cloud_done : Icons.cloud_off,
                  color: status == '在线' ? Colors.green : Colors.red,
                  size: 18,
                ),
                const SizedBox(width: 8),
                Text('诊断记录 ${diagnoses.length} 条 · $status'),
              ],
            ),
          ),
          Expanded(
            child: RefreshIndicator(
              onRefresh: load,
              child: diagnoses.isEmpty
                  ? ListView(
                      physics: const AlwaysScrollableScrollPhysics(),
                      children: [
                        SizedBox(
                          height: 240,
                          child: Center(child: Text(loading ? '正在加载' : '暂无诊断记录')),
                        ),
                      ],
                    )
                  : ListView.separated(
                      physics: const AlwaysScrollableScrollPhysics(),
                      itemCount: diagnoses.length,
                      separatorBuilder: (context, index) => const Divider(height: 1),
                      itemBuilder: (context, index) {
                        final diagnosis = diagnoses[index];
                        final faultLevel = diagnosis.fault == 'normal'
                            ? 'normal'
                            : diagnosis.fault == 'imbalance' || diagnosis.fault == 'looseness'
                                ? 'warning'
                                : 'critical';
                        return ListTile(
                          leading: Icon(levelIcon(faultLevel), color: levelColor(faultLevel)),
                          title: Text('${diagnosis.deviceId} · ${diagnosis.fault}'),
                          subtitle: Text(
                            '主频 ${diagnosis.dominantHz.toStringAsFixed(1)} Hz  '
                            '低/中/高频 ${diagnosis.lowBand.toStringAsFixed(2)} / '
                            '${diagnosis.midBand.toStringAsFixed(2)} / '
                            '${diagnosis.highBand.toStringAsFixed(2)}\n'
                            '${diagnosis.diagnosedAt}',
                          ),
                          isThreeLine: true,
                        );
                      },
                    ),
            ),
          ),
        ],
      ),
    );
  }
}
