import 'dart:convert';
import 'dart:typed_data';

import 'package:connectme/core/network/api_client.dart';
import 'package:connectme/core/network/token_store.dart';
import 'package:dio/dio.dart';

class Reply {
  Reply(this.status, [this.body = const {}, this.headers = const {}]);
  final int status;
  final Object body;
  final Map<String, String> headers;
}

typedef Handler = Future<Reply> Function(RequestOptions o);

/// A Dio adapter that answers from a function, recording every request.
class FakeAdapter implements HttpClientAdapter {
  FakeAdapter(this.handler);
  final Handler handler;
  final List<RequestOptions> requests = [];

  int count(String path) => requests.where((r) => r.path == path).length;

  @override
  Future<ResponseBody> fetch(RequestOptions options, Stream<Uint8List>? requestStream, Future<void>? cancelFuture) async {
    requests.add(options);
    final r = await handler(options);
    return ResponseBody.fromString(jsonEncode(r.body), r.status, headers: {
      Headers.contentTypeHeader: ['application/json'],
      for (final e in r.headers.entries) e.key.toLowerCase(): [e.value],
    });
  }

  @override
  void close({bool force = false}) {}
}

class NetworkDown implements HttpClientAdapter {
  @override
  Future<ResponseBody> fetch(RequestOptions options, Stream<Uint8List>? requestStream, Future<void>? cancelFuture) =>
      throw DioException.connectionError(requestOptions: options, reason: 'down');
  @override
  void close({bool force = false}) {}
}

/// An ApiClient wired to fake adapters for the API and for the refresh call.
ApiClient fakeClient(
  Handler handler, {
  TokenStore? tokens,
  Handler? refresh,
  void Function()? onExpired,
  FakeAdapter? adapterOut,
}) {
  final dio = Dio(BaseOptions(baseUrl: 'http://test'))..httpClientAdapter = FakeAdapter(handler);
  final rdio = Dio(BaseOptions(baseUrl: 'http://test'))
    ..httpClientAdapter = FakeAdapter(refresh ?? (_) async => Reply(401, {'error': 'no'}));
  return ApiClient(baseUrl: 'http://test', tokens: tokens ?? MemoryTokenStore(), dio: dio, refreshDio: rdio, onSessionExpired: onExpired);
}
