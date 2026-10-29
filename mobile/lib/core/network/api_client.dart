import 'dart:async';
import 'dart:typed_data';

import 'package:dio/dio.dart';

import 'token_store.dart';

/// Error code the server sends when content moderation cannot run (HTTP 503).
const moderationUnavailableCode = 'moderation_unavailable';

/// What a person is told when content could not be checked (English fallback;
/// the localized text comes from AppLocalizations.sModerationUnavailable).
const moderationUnavailableMessage = "We can't check this right now. Please try again in a moment.";

/// An error from the API or the network, with a message safe to show.
class ApiException implements Exception {
  ApiException(this.status, this.message, {this.retryAfter, this.code});
  final int status;
  final String message;
  final int? retryAfter;

  /// The server's machine-readable error code, when it sent one we know.
  final String? code;

  /// The server could not check public content (bio, photo, note) right now.
  bool get isModerationUnavailable => code == moderationUnavailableCode;

  bool get isUnauthorized => status == 401;
  bool get isNetwork => status == 0;
  bool get isRateLimited => status == 429;
  bool get isPaymentRequired => status == 402;

  @override
  String toString() => message;
}

/// Thin JSON client over Dio.
///
/// - Attaches the access token.
/// - On 401 refreshes the session once (single-flight: concurrent requests
///   share one refresh) and retries the request.
/// - If refresh fails the session is cleared and [onSessionExpired] fires.
class ApiClient {
  ApiClient({
    required String baseUrl,
    required this.tokens,
    this.onSessionExpired,
    Dio? dio,
    Dio? refreshDio,
  })  : dio = dio ?? Dio(BaseOptions(baseUrl: baseUrl)),
        _refreshDio = refreshDio ?? Dio(BaseOptions(baseUrl: baseUrl)) {
    this.dio.options
      ..connectTimeout = const Duration(seconds: 10)
      ..receiveTimeout = const Duration(seconds: 20);
    this.dio.interceptors.add(_AuthInterceptor(this));
  }

  final Dio dio;
  final Dio _refreshDio;

  /// Retries run on a second Dio that shares the adapter but has NO
  /// interceptors. Re-entering [dio] from inside its own queued error
  /// interceptor would deadlock if the retry also returned 401.
  late final Dio _retryDio = Dio(dio.options.copyWith())..httpClientAdapter = dio.httpClientAdapter;
  final TokenStore tokens;
  final void Function()? onSessionExpired;
  Future<bool>? _refreshing;

  /// Single-flight refresh. Returns true when a new token pair is stored.
  Future<bool> refresh() => _refreshing ??= _doRefresh().whenComplete(() => _refreshing = null);

  Future<bool> _doRefresh() async {
    final t = await tokens.read();
    if (t == null) return false;
    try {
      final r = await _refreshDio.post('/auth/refresh',
          data: {'refresh_token': t.refresh},
          options: Options(validateStatus: (_) => true));
      if (r.statusCode == 200 && r.data is Map) {
        final m = r.data as Map;
        await tokens.write(Tokens(
            access: m['access_token'] as String,
            refresh: m['refresh_token'] as String));
        return true;
      }
    } on DioException {
      // Offline: keep the session, let the caller see a network error.
      return false;
    }
    await tokens.clear();
    onSessionExpired?.call();
    return false;
  }

  Future<dynamic> request(
    String method,
    String path, {
    Object? body,
    Map<String, dynamic>? query,
    Options? options,
  }) async {
    try {
      final r = await dio.request<dynamic>(
        path,
        data: body,
        queryParameters: query,
        options: (options ?? Options()).copyWith(method: method),
      );
      final code = r.statusCode ?? 0;
      if (code >= 200 && code < 300) return r.data;
      throw _fromResponse(r);
    } on DioException catch (e) {
      if (e.response != null) throw _fromResponse(e.response!);
      throw ApiException(0, 'No connection. Check your internet and try again.');
    }
  }

  Future<Map<String, dynamic>> getMap(String path, {Map<String, dynamic>? query}) async =>
      _map(await request('GET', path, query: query));

  Future<Map<String, dynamic>> postMap(String path, {Object? body}) async =>
      _map(await request('POST', path, body: body));

  Future<void> post(String path, {Object? body}) async => request('POST', path, body: body);
  Future<void> delete(String path) async => request('DELETE', path);

  Future<Map<String, dynamic>> patchMap(String path, {Object? body}) async =>
      _map(await request('PATCH', path, body: body));

  Future<void> put(String path, {Object? body}) async => request('PUT', path, body: body);

  Future<Map<String, dynamic>> putMap(String path, {Object? body}) async => _map(await request('PUT', path, body: body));

  Future<Uint8List> getBytes(String path) async {
    final r = await request('GET', path, options: Options(responseType: ResponseType.bytes));
    return Uint8List.fromList((r as List).cast<int>());
  }

  Future<Map<String, dynamic>> postBytes(String path, Uint8List data) async => _map(await request(
        'POST',
        path,
        body: Stream.fromIterable([data]),
        options: Options(headers: {
          Headers.contentLengthHeader: data.length,
          Headers.contentTypeHeader: 'application/octet-stream',
        }),
      ));

  /// Uploads raw bytes with PUT (used for voice intros).
  Future<void> putBytes(String path, Uint8List data, {Map<String, dynamic>? query}) async => request(
        'PUT',
        path,
        query: query,
        body: Stream.fromIterable([data]),
        options: Options(headers: {
          Headers.contentLengthHeader: data.length,
          Headers.contentTypeHeader: 'application/octet-stream',
        }),
      );

  Future<Map<String, dynamic>> postForm(String path, FormData form) async =>
      _map(await request('POST', path, body: form));

  Map<String, dynamic> _map(dynamic d) => d is Map ? Map<String, dynamic>.from(d) : <String, dynamic>{};

  ApiException _fromResponse(Response r) {
    var msg = 'Something went wrong. Please try again.';
    final d = r.data;
    if (d is Map && d['error'] is String) msg = d['error'] as String;
    final ra = int.tryParse(r.headers.value('retry-after') ?? '');
    if (r.statusCode == 503 && msg == moderationUnavailableCode) {
      return ApiException(503, moderationUnavailableMessage, retryAfter: ra ?? 5, code: moderationUnavailableCode);
    }
    return ApiException(r.statusCode ?? 0, msg, retryAfter: ra);
  }
}

class _AuthInterceptor extends QueuedInterceptor {
  _AuthInterceptor(this.c);
  final ApiClient c;

  static const _usedToken = 'used_access_token';
  static const _retried = 'retried_after_refresh';

  @override
  Future<void> onRequest(RequestOptions o, RequestInterceptorHandler h) async {
    // Auth endpoints that need no session must not carry (or trigger refresh of) one.
    final t = await c.tokens.read();
    if (t != null && !o.path.startsWith('/auth/')) {
      o.headers['Authorization'] = 'Bearer ${t.access}';
      o.extra[_usedToken] = t.access;
    }
    h.next(o);
  }

  @override
  Future<void> onError(DioException e, ErrorInterceptorHandler h) async {
    final o = e.requestOptions;
    if (e.response?.statusCode != 401 || o.path.startsWith('/auth/') || o.extra[_retried] == true) {
      return h.next(e);
    }
    // Another queued request may already have refreshed: compare tokens first.
    final current = await c.tokens.read();
    final used = o.extra[_usedToken];
    var ok = current != null && used != null && current.access != used;
    if (!ok) ok = await c.refresh();
    if (!ok) return h.next(e);
    final t = await c.tokens.read();
    o.headers['Authorization'] = 'Bearer ${t!.access}';
    o.extra[_usedToken] = t.access;
    o.extra[_retried] = true;
    try {
      h.resolve(await c._retryDio.fetch(o));
    } on DioException catch (err) {
      h.next(err);
    }
  }
}

/// Provider retry policy (passed to `ProviderScope.retry`).
///
/// Riverpod 3 retries every failed provider up to 10 times with backoff while
/// reporting "loading", so a plain 404 or 403 looked like an endless spinner.
/// Only failures that can heal on their own are retried, and only briefly:
/// no connection (status 0) and server errors (5xx), at most twice.
Duration? appRetry(int retryCount, Object error) {
  if (retryCount >= 2 || error is! ApiException) return null;
  if (error.isNetwork || error.status >= 500) return Duration(seconds: retryCount + 1);
  return null; // 4xx: retrying cannot change the answer
}
