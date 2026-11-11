import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

void main() => runApp(const PulseApp());

class PulseApp extends StatelessWidget {
  const PulseApp({super.key});
  @override
  Widget build(BuildContext context) {
    return MaterialApp(title: 'PulseMesh', theme: ThemeData(useMaterial3: true), home: const HomePage());
  }
}

class HomePage extends StatefulWidget {
  const HomePage({super.key});
  @override
  State<HomePage> createState() => _HomePageState();
}

class _HomePageState extends State<HomePage> {
  List samples = [];
  String status = 'loading';
  Future<void> load() async {
    try {
      final r = await http.get(Uri.parse('http://10.0.2.2:8080/api/v1/samples')).timeout(const Duration(seconds: 5));
      setState(() { samples = jsonDecode(r.body) as List; status = 'ok'; });
    } catch (e) {
      setState(() { status = 'offline: $e'; });
    }
  }
  @override
  void initState() { super.initState(); load(); }
  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('PulseMesh 工业巡检')),
      floatingActionButton: FloatingActionButton(onPressed: load, child: const Icon(Icons.refresh)),
      body: Column(children: [
        Padding(padding: const EdgeInsets.all(12), child: Text('cloud: $status  samples: ${samples.length}')),
        Expanded(child: ListView.builder(itemCount: samples.length, itemBuilder: (c, i) {
          final s = samples[i];
          return ListTile(title: Text('${s['device_id']}  ${s['metric']}'), subtitle: Text('${s['value']}  ${s['ts'] ?? ''}'), trailing: const Icon(Icons.sensors));
        })),
      ]),
    );
  }
}
