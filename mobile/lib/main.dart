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
  String? selectedDevice;
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
    final devices = samples.map((sample) => sample.deviceId).toSet().toList()..sort();
    final visibleSamples = (selectedDevice == null
            ? samples
            : samples.where((sample) => sample.deviceId == selectedDevice))
        .toList();
    final latestByDevice = <String, Sample>{};
    for (final sample in visibleSamples) {
      final current = latestByDevice[sample.deviceId];
      if (current == null || _sampleTime(sample).isAfter(_sampleTime(current))) {
        latestByDevice[sample.deviceId] = sample;
      }
    }
    final latestSamples = latestByDevice.values.toList()
      ..sort((left, right) => left.deviceId.compareTo(right.deviceId));
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
            child: Wrap(
              crossAxisAlignment: WrapCrossAlignment.center,
              spacing: 12,
              children: [
                Icon(
                  status == 'ok' ? Icons.cloud_done : Icons.cloud_off,
                  color: status == 'ok' ? Colors.green : Colors.red,
                  size: 18,
                ),
                Text('云端 $status · ${visibleSamples.length} 条'),
                DropdownButton<String?>(
                  value: selectedDevice,
                  hint: const Text('全部设备'),
                  items: [
                    const DropdownMenuItem<String?>(
                      value: null,
                      child: Text('全部设备'),
                    ),
                    ...devices.map((device) => DropdownMenuItem<String?>(
                          value: device,
                          child: Text(device),
                        )),
                  ],
                  onChanged: (device) => setState(() => selectedDevice = device),
                ),
              ],
            ),
          ),
          if (latestSamples.isNotEmpty)
            SizedBox(
              height: 116,
              child: ListView.separated(
                padding: const EdgeInsets.symmetric(horizontal: 12),
                scrollDirection: Axis.horizontal,
                itemCount: latestSamples.length,
                separatorBuilder: (context, index) => const SizedBox(width: 8),
                itemBuilder: (context, index) {
                  final latest = latestSamples[index];
                  return SizedBox(
                    width: 190,
                    child: Card(
                      child: InkWell(
                        borderRadius: BorderRadius.circular(12),
                        onTap: () => setState(() => selectedDevice = latest.deviceId),
                        child: Padding(
                          padding: const EdgeInsets.all(12),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            mainAxisAlignment: MainAxisAlignment.center,
                            children: [
                              Text(latest.deviceId, maxLines: 1, overflow: TextOverflow.ellipsis),
                              const SizedBox(height: 6),
                              Row(
                                children: [
                                  Icon(levelIcon(latest.level), color: levelColor(latest.level)),
                                  const SizedBox(width: 6),
                                  Text(
                                    '${latest.value.toStringAsFixed(2)} · ${latest.level}',
                                    style: TextStyle(color: levelColor(latest.level)),
                                  ),
                                ],
                              ),
                            ],
                          ),
                        ),
                      ),
                    ),
                  );
                },
              ),
            ),
          Expanded(
            child: RefreshIndicator(
              onRefresh: load,
              child: visibleSamples.isEmpty
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
                      itemCount: visibleSamples.length,
                      itemBuilder: (context, index) {
                        final s = visibleSamples[index];
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

DateTime _sampleTime(Sample sample) => DateTime.tryParse(sample.ts) ?? DateTime.fromMillisecondsSinceEpoch(0);

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
    final openAlerts = alerts.where((item) => !item.acked).toList();
    final acknowledgedAlerts = alerts.where((item) => item.acked).toList();
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
                Text('未确认 ${openAlerts.length} 条 · 已确认 ${acknowledgedAlerts.length} 条'),
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
                  : ListView(
                      physics: const AlwaysScrollableScrollPhysics(),
                      children: [
                        if (openAlerts.isNotEmpty) ...[
                          _alertSectionTitle('待处理', openAlerts.length),
                          ...openAlerts.map(_buildAlertTile),
                        ],
                        if (acknowledgedAlerts.isNotEmpty) ...[
                          _alertSectionTitle('已确认', acknowledgedAlerts.length),
                          ...acknowledgedAlerts.map(_buildAlertTile),
                        ],
                      ],
                    ),
          ),
        ],
      ),
    );
  }

  Widget _alertSectionTitle(String title, int count) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
      child: Text('$title ($count)', style: Theme.of(context).textTheme.titleSmall),
    );
  }

  Widget _buildAlertTile(AlertItem alert) {
    return Column(
      children: [
        ListTile(
          leading: Icon(levelIcon(alert.level), color: levelColor(alert.level)),
          title: Text(alert.message),
          subtitle: Text('${alert.deviceId} · ${alert.metric}'),
          trailing: alert.acked
              ? const Chip(label: Text('已确认'))
              : acknowledging.contains(alert.id)
                  ? const SizedBox(
                      width: 24,
                      height: 24,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : IconButton(
                      icon: const Icon(Icons.done_all),
                      tooltip: '确认',
                      onPressed: () => ack(alert),
                    ),
        ),
        const Divider(height: 1),
      ],
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
                        return Card(
                          margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                          child: Padding(
                            padding: const EdgeInsets.all(12),
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                ListTile(
                                  contentPadding: EdgeInsets.zero,
                                  leading: Icon(levelIcon(faultLevel), color: levelColor(faultLevel)),
                                  title: Text('${diagnosis.deviceId} · ${diagnosis.fault}'),
                                  subtitle: Text(diagnosis.diagnosedAt),
                                ),
                                Text(
                                  '主频 ${diagnosis.dominantHz.toStringAsFixed(1)} Hz',
                                  style: Theme.of(context).textTheme.titleMedium,
                                ),
                                const SizedBox(height: 8),
                                _SpectrumBands(diagnosis: diagnosis),
                              ],
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

class _SpectrumBands extends StatelessWidget {
  const _SpectrumBands({required this.diagnosis});

  final Diagnosis diagnosis;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final bands = [
      _SpectrumBand('低频', diagnosis.lowBand, colors.primary),
      _SpectrumBand('中频', diagnosis.midBand, const Color(0xFFEF6C00)),
      _SpectrumBand('高频', diagnosis.highBand, const Color(0xFFC62828)),
    ];
    return Column(
      children: [
        for (final band in bands)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 3),
            child: Row(
              children: [
                SizedBox(width: 42, child: Text(band.label)),
                Expanded(
                  child: ClipRRect(
                    borderRadius: BorderRadius.circular(4),
                    child: LinearProgressIndicator(
                      value: band.ratio.clamp(0.0, 1.0),
                      minHeight: 10,
                      color: band.color,
                      backgroundColor: Theme.of(context).colorScheme.surfaceContainerHighest,
                    ),
                  ),
                ),
                SizedBox(
                  width: 52,
                  child: Text('${(band.ratio * 100).toStringAsFixed(1)}%', textAlign: TextAlign.end),
                ),
              ],
            ),
          ),
        Align(
          alignment: Alignment.centerLeft,
          child: Text('峰值幅度 ${diagnosis.peakAmplitude.toStringAsFixed(3)}'),
        ),
      ],
    );
  }
}

class _SpectrumBand {
  const _SpectrumBand(this.label, this.ratio, this.color);

  final String label;
  final double ratio;
  final Color color;
}
