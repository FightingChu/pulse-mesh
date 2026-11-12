import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:pulse_mesh_mobile/api.dart';

void main() {
  test('samples decodes the API payload', () async {
    final client = ApiClient(MockClient((request) async {
      expect(request.url.path, '/api/v1/samples');
      return http.Response(
        jsonEncode([
          {
            'device_id': 'motor-01',
            'metric': 'vibration_rms',
            'value': 1.25,
            'level': 'normal',
            'ts': '2026-11-12T00:00:00Z',
          }
        ]),
        200,
      );
    }));
    addTearDown(client.close);

    final samples = await client.samples();
    expect(samples, hasLength(1));
    expect(samples.single.deviceId, 'motor-01');
    expect(samples.single.value, 1.25);
  });

  test('alerts includes acknowledged filter', () async {
    final client = ApiClient(MockClient((request) async {
      expect(request.url.queryParameters['include_acked'], '1');
      return http.Response('[]', 200);
    }));
    addTearDown(client.close);

    expect(await client.alerts(includeAcked: true), isEmpty);
  });

  test('ack throws API error on non-success response', () async {
    final client = ApiClient(MockClient((request) async {
      expect(request.method, 'POST');
      expect(request.url.path, '/api/v1/alerts/alert-4/ack');
      return http.Response('{"error":"alert not found"}', 404);
    }));
    addTearDown(client.close);

    await expectLater(
      client.ack('alert-4'),
      throwsA(
        isA<ApiException>()
            .having((error) => error.statusCode, 'statusCode', 404)
            .having((error) => error.message, 'message', 'alert not found'),
      ),
    );
  });
}
