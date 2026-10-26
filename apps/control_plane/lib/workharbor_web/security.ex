# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.Security do
  @moduledoc false
  import Plug.Conn
  def init(opts), do: opts

  def call(conn, _) do
    config = Application.fetch_env!(:workharbor, :owner_http)

    with true <- conn.scheme == :https,
         true <- authority?(conn, Keyword.fetch!(config, :authorities)),
         true <- origin?(conn),
         {:ok, target, store} <- context(conn),
         true <- socket_origin?(conn),
         :ok <- canonical_path(conn),
         :ok <- query_admission(conn),
         true <- upgrade_headers?(conn),
         :ok <- asset_admission(conn) do
      conn |> put_private(:auth_target, target) |> put_private(:auth_store, store) |> headers()
    else
      {:error, :invalid_request} -> deny(conn, 400, :invalid_request)
      {:error, :not_found} -> deny(conn, 404, :not_found)
      _ -> deny(conn, 403, :forbidden)
    end
  end

  def headers(conn) do
    conn
    |> put_resp_header("cache-control", "no-store")
    |> put_resp_header("x-content-type-options", "nosniff")
    |> put_resp_header("x-frame-options", "DENY")
    |> put_resp_header(
      "content-security-policy",
      "default-src 'none'; script-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"
    )
    |> put_resp_header("referrer-policy", "no-referrer")
    |> put_resp_header("strict-transport-security", "max-age=31536000")
  end

  def owner_headers(conn) do
    origin = Keyword.fetch!(Application.fetch_env!(:workharbor, :owner_http), :origin)

    case WorkHarborWeb.Authority.origin(origin) do
      {:ok, {host, port}} ->
        authority = if String.contains?(host, ":"), do: "[" <> host <> "]", else: host

        authority =
          if port == 443, do: authority, else: authority <> ":" <> Integer.to_string(port)

        csp =
          "default-src 'none'; script-src 'self'; style-src 'self'; " <>
            "connect-src 'self' wss://" <>
            authority <>
            "; img-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"

        conn |> headers() |> put_resp_header("content-security-policy", csp)

      _ ->
        deny(conn, 503, :unavailable)
    end
  end

  defp asset_admission(conn) do
    cond do
      conn.request_path in ["/assets/js/app.js", "/assets/css/app.css"] ->
        if conn.method in ["GET", "HEAD"], do: :ok, else: {:error, :not_found}

      String.starts_with?(conn.request_path, ["/assets", "/images", "/fonts"]) or
          conn.request_path in ["/robots.txt", "/favicon.ico"] ->
        {:error, :not_found}

      true ->
        :ok
    end
  end

  def deny(conn, status, code),
    do:
      conn
      |> headers()
      |> put_resp_content_type("application/json")
      |> send_resp(status, Jason.encode!(%{error: %{code: code}}))
      |> halt()

  def own_origin?(conn) do
    expected =
      WorkHarborWeb.Authority.origin(
        Keyword.fetch!(Application.fetch_env!(:workharbor, :owner_http), :origin)
      )

    case get_req_header(conn, "origin") do
      [origin] -> expected != :error and WorkHarborWeb.Authority.origin(origin) == expected
      _ -> false
    end
  end

  def socket_origin(uri) do
    WorkHarborWeb.Authority.origin(URI.to_string(uri)) ==
      WorkHarborWeb.Authority.origin(
        Keyword.fetch!(Application.fetch_env!(:workharbor, :owner_http), :origin)
      )
  end

  defp origin?(conn), do: get_req_header(conn, "origin") == [] or own_origin?(conn)

  defp socket?(conn),
    do: conn.request_path in ["/owner/live/websocket", "/dashboard/live/websocket"]

  defp socket_origin?(conn), do: not socket?(conn) or own_origin?(conn)

  defp authority?(conn, allowed) do
    with {:ok, effective} <- WorkHarborWeb.Authority.effective(conn.host, conn.port),
         true <-
           Enum.any?(allowed, fn {host, port} ->
             WorkHarborWeb.Authority.effective(host, port) == {:ok, effective}
           end) do
      case get_req_header(conn, "host") do
        [] -> get_http_protocol(conn) == :"HTTP/2"
        [h] -> WorkHarborWeb.Authority.parse(h) == {:ok, effective}
        _ -> false
      end
    else
      _ -> false
    end
  end

  defp canonical_path(conn) do
    path = conn.request_path

    if String.contains?(path, ["%", "//", "\\"]) or
         Enum.any?(String.split(path, "/"), &(&1 in [".", ".."])) or
         (path != "/" and String.ends_with?(path, "/")),
       do: {:error, :invalid_request},
       else: :ok
  end

  # Fixed current auth/control routes have no query parameters. Socket transport
  # allows only its two non-credential parameters, decoded before duplicate loss.
  defp query_admission(conn) do
    cond do
      socket?(conn) ->
        case WorkHarborWeb.LoginForm.decode(conn.query_string, ["_csrf_token", "vsn"]) do
          {:ok, %{"_csrf_token" => csrf, "vsn" => "2.0.0"}} when byte_size(csrf) == 56 -> :ok
          _ -> {:error, :invalid_request}
        end

      conn.request_path == "/owner/hosts" ->
        case owner_list_params(conn.query_string) do
          {:ok, _} -> :ok
          _ -> {:error, :invalid_request}
        end

      conn.query_string == "" ->
        :ok

      true ->
        {:error, :invalid_request}
    end
  end

  @doc "Closed pagination admission shared by the transport and host page."
  def owner_list_params(""), do: {:ok, %{page: 1, size: 25}}

  def owner_list_params(raw) when is_binary(raw) and byte_size(raw) <= 128 do
    pairs = String.split(raw, "&")

    with true <- length(pairs) <= 2,
         true <- Enum.all?(pairs, &Regex.match?(~r/\A(page|size)=[0-9]+\z/, &1)),
         values <-
           Enum.map(pairs, fn pair ->
             [key, value] = String.split(pair, "=")
             {key, String.to_integer(value)}
           end),
         true <- length(Enum.uniq_by(values, &elem(&1, 0))) == length(values),
         page <- Map.get(Map.new(values), "page", 1),
         size <- Map.get(Map.new(values), "size", 25),
         true <- page in 1..1000 and size in 1..50 do
      {:ok, %{page: page, size: size}}
    else
      _ -> {:error, :invalid_request}
    end
  end

  def owner_list_params(_), do: {:error, :invalid_request}

  # The stock adapter reports malformed upgrade header values in its exception.
  # Admit only known header shapes so those diagnostics cannot echo credentials.
  defp upgrade_headers?(conn) do
    not socket?(conn) or
      (get_req_header(conn, "sec-websocket-version") == ["13"] and
         header_token?(conn, "connection", "upgrade") and
         header_token?(conn, "upgrade", "websocket") and
         case get_req_header(conn, "sec-websocket-key") do
           [key] ->
             case Base.decode64(key) do
               {:ok, bytes} -> byte_size(bytes) == 16 and Base.encode64(bytes) == key
               _ -> false
             end

           _ ->
             false
         end)
  end

  defp header_token?(conn, name, expected) do
    case get_req_header(conn, name) do
      [value] -> expected in (value |> String.downcase() |> Plug.Conn.Utils.list())
      _ -> false
    end
  end

  defp context(conn) do
    target =
      cond do
        conn.request_path == "/" ->
          :owner

        conn.request_path == "/login/dashboard/clear" ->
          :dashboard

        String.starts_with?(conn.request_path, "/login/owner") ->
          :owner

        String.starts_with?(conn.request_path, "/login/dashboard") or
            conn.request_path == "/login/read-token" ->
          :dashboard

        String.starts_with?(conn.request_path, "/owner") ->
          :owner

        String.starts_with?(conn.request_path, "/dashboard") ->
          :dashboard

        true ->
          nil
      end

    values = get_req_header(conn, "x-workharbor-context")

    header =
      case values do
        [] -> nil
        ["owner"] -> :owner
        ["dashboard"] -> :dashboard
        _ -> :invalid
      end

    cond do
      header == :invalid ->
        {:error, :context}

      target && header && target != header ->
        {:error, :context}

      true ->
        target = target || header

        store =
          if String.starts_with?(conn.request_path, "/login/") and
               conn.request_path != "/login/dashboard/clear",
             do: :preauth,
             else: target || :preauth

        {:ok, target, store}
    end
  end
end

defmodule WorkHarborWeb.BrowserSession do
  @moduledoc false
  import Plug.Conn

  @names %{
    owner: "__Host-workharbor_owner",
    dashboard: "__Host-workharbor_dashboard",
    preauth: "__Host-workharbor_login"
  }
  def options(context) do
    [
      store: :cookie,
      key: Map.fetch!(@names, context),
      signing_salt: "workharbor-#{context}-sign-v1",
      encryption_salt: "workharbor-#{context}-encrypt-v1",
      secure: true,
      http_only: true,
      same_site: "Strict",
      path: "/",
      max_age: if(context == :preauth, do: 600, else: 28_800),
      log: false
    ]
  end

  def init(opts), do: opts

  def call(conn, _) do
    conn = register_before_send(conn, &cap_cookie/1)
    Plug.Session.call(conn, Plug.Session.init(options(conn.private.auth_store)))
  end

  defp cap_cookie(conn) do
    store = conn.private.auth_store
    key = Map.fetch!(@names, store)

    case {store, conn.assigns[:principal], conn.resp_cookies[key]} do
      {s, %{expires_at: expiry}, %{max_age: age} = cookie}
      when s in [:owner, :dashboard] and age > 0 ->
        cookie = Map.put(cookie, :max_age, min(age, max(expiry - WorkHarbor.Accounts.now(), 0)))
        %{conn | resp_cookies: Map.put(conn.resp_cookies, key, cookie)}

      _ ->
        conn
    end
  end

  def install(conn, context, token, expiry) do
    opts = options(context)

    value =
      Plug.Session.COOKIE.put(conn, nil, %{"credential" => token}, Plug.Session.COOKIE.init(opts))

    cookie_opts =
      Keyword.take(opts, [:secure, :http_only, :same_site, :path])
      |> Keyword.put(:max_age, max(expiry - WorkHarbor.Accounts.now(), 0))

    put_resp_cookie(conn, Keyword.fetch!(opts, :key), value, cookie_opts)
  end

  def auth_cookies?(conn) do
    conn = fetch_cookies(conn)
    Enum.any?([:owner, :dashboard], &Map.has_key?(conn.req_cookies, @names[&1]))
  end

  def selected_csrf?(conn, token) do
    state = Plug.CSRFProtection.dump_state_from_session(get_session(conn, "_csrf_token"))

    is_binary(state) and is_binary(token) and
      Plug.CSRFProtection.valid_state_and_csrf_token?(state, token)
  end

  def initialize_preauth(conn) do
    now = WorkHarbor.Accounts.now()

    conn
    |> clear_session()
    |> configure_session(renew: true)
    |> put_session("issued_at", now)
    |> put_session("expires_at", now + 600)
  end

  def valid_preauth?(conn) do
    issued = get_session(conn, "issued_at")
    expiry = get_session(conn, "expires_at")
    now = WorkHarbor.Accounts.now()

    is_integer(issued) and is_integer(expiry) and expiry - issued == 600 and issued <= now and
      expiry > now
  end
end

defmodule WorkHarborWeb.Body do
  @moduledoc false
  import Plug.Conn

  def parse(conn, :json) do
    parse(
      conn,
      1_048_576,
      ~r/^application\/json(?:;\s*charset=(?:utf-8|"utf-8"))?$/i,
      &WorkHarbor.StrictJSON.decode_object/1
    )
  end

  def parse(conn, {:form, fields}) do
    parse(
      conn,
      4096,
      ~r/^application\/x-www-form-urlencoded(?:;\s*charset=(?:utf-8|"utf-8"))?$/i,
      &WorkHarborWeb.LoginForm.decode(&1, fields)
    )
  end

  defp parse(conn, limit, type, decoder) do
    with [header] <- get_req_header(conn, "content-type"), true <- Regex.match?(type, header) do
      case read(conn, limit, [], 0, System.monotonic_time(:millisecond) + 15_000) do
        {:ok, body, conn} ->
          case decoder.(body) do
            {:ok, value} -> {:ok, value, conn}
            {:error, e} -> {:error, e, conn}
          end

        error ->
          error
      end
    else
      _ -> {:error, :unsupported_media_type, conn}
    end
  end

  defp read(conn, limit, parts, size, deadline) do
    if System.monotonic_time(:millisecond) > deadline do
      {:error, :invalid_request, conn}
    else
      case read_body(conn, length: 16_384, read_length: 16_384, read_timeout: 5000) do
        {status, data, conn} when status in [:more, :ok] ->
          size = size + byte_size(data)

          cond do
            size > limit -> {:error, :input_limit_exceeded, conn}
            status == :ok -> {:ok, IO.iodata_to_binary(Enum.reverse([data | parts])), conn}
            true -> read(conn, limit, [data | parts], size, deadline)
          end

        _ ->
          {:error, :invalid_request, conn}
      end
    end
  end
end
