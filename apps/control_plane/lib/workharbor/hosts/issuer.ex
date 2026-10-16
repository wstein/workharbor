# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.Hosts.Issuer do
  @moduledoc "Native client identity issuer; initialization is explicit and exclusive."
  require Record

  for {name, tag} <- [
        tbs: :OTPTBSCertificate,
        cert: :OTPCertificate,
        validity: :Validity,
        signature: :SignatureAlgorithm,
        public_algorithm: :PublicKeyAlgorithm,
        spki: :OTPSubjectPublicKeyInfo,
        extension: :Extension,
        basic: :BasicConstraints
      ] do
    Record.defrecordp(
      name,
      tag,
      Record.extract(tag, from_lib: "public_key/include/public_key.hrl")
    )
  end

  @ed {1, 3, 101, 112}
  @day 86400
  # Fixed leaf lifetime (seconds); also bounds how long a replaced issuer's leaves can live.
  def leaf_lifetime, do: @day

  def initialize(dir) do
    path = Path.join(dir, "issuer.json")

    with true <- Path.type(dir) == :absolute,
         :ok <- WorkHarbor.Hosts.PrivateFile.traversal(dir, true),
         :ok <- ensure_dir(dir),
         :ok <- WorkHarbor.Hosts.PrivateFile.directory(dir) do
      key = :public_key.generate_key({:namedCurve, @ed})
      public = elem(key, 4)
      now = WorkHarbor.Accounts.now()
      name = subject("WorkHarbor client issuer")
      certificate = sign(key, public, name, name, now, now + 5 * 365 * @day, true)

      state = %{
        "schema_version" => 1,
        "certificate" => Base.encode64(certificate),
        "private_key" => Base.encode64(:public_key.der_encode(:PrivateKeyInfo, key))
      }

      case WorkHarbor.Hosts.PrivateFile.create(path, Jason.encode!(state)) do
        :ok -> load(dir)
        error -> error
      end
    else
      _ -> {:error, :unsafe_identity}
    end
  rescue
    _ -> {:error, :invalid_identity}
  end

  def load(dir) do
    path = Path.join(dir, "issuer.json")

    if not File.exists?(path) do
      {:error, :invalid_identity}
    else
      with {:ok, bytes} <- WorkHarbor.Hosts.PrivateFile.read(path),
           {:ok,
            %{"schema_version" => 1, "certificate" => cert64, "private_key" => key64} = state} <-
             WorkHarbor.StrictJSON.decode_object(bytes),
           true <- map_size(state) == 3,
           {:ok, der} <- Base.decode64(cert64),
           {:ok, key_der} <- Base.decode64(key64),
           true <- Base.encode64(der) == cert64 and Base.encode64(key_der) == key64 do
        key = :public_key.der_decode(:PrivateKeyInfo, key_der)
        if :public_key.der_encode(:PrivateKeyInfo, key) != key_der, do: throw(:invalid_identity)
        public = elem(key, 4)

        public =
          if public == :asn1_NOVALUE,
            do: elem(:crypto.generate_key(:eddsa, :ed25519, elem(key, 2)), 0),
            else: public

        info = inspect_certificate(der)
        now = WorkHarbor.Accounts.now()

        if elem(key, 3) != {:namedCurve, @ed} or info.public != public or not root_profile?(info) or
             info.not_before > now or info.not_after < now + @day or
             info.subject != subject("WorkHarbor client issuer") or
             not :public_key.pkix_verify(der, {{:ECPoint, public}, {:namedCurve, @ed}}),
           do: throw(:invalid_identity)

        {:ok,
         %{
           key: key,
           certificate: der,
           public: public,
           subject: info.subject,
           expires_at: info.not_after
         }}
      else
        {:error, :unsafe_identity} = error -> error
        _ -> {:error, :invalid_identity}
      end
    end
  rescue
    _ -> {:error, :invalid_identity}
  catch
    _ -> {:error, :invalid_identity}
  end

  def valid_issuer?(der) do
    info = inspect_certificate(der)
    now = WorkHarbor.Accounts.now()

    root_profile?(info) and info.not_before <= now and info.not_after >= now + @day and
      :public_key.pkix_verify(der, {{:ECPoint, info.public}, {:namedCurve, @ed}})
  rescue
    _ -> false
  catch
    _ -> false
  end

  def issue(issuer, csr, host, mode, now \\ WorkHarbor.Accounts.now()) do
    with {:ok, public} <- csr_public(csr, if(mode == :enrollment, do: nil, else: host)),
         true <- issuer.expires_at >= now + @day do
      {:ok, sign(issuer.key, public, issuer.subject, subject(host), now, now + @day, false)}
    else
      {:error, _} = e -> e
      _ -> {:error, :issuer_unavailable}
    end
  rescue
    _ -> {:error, :invalid_csr}
  end

  def csr_public(bytes, host) when is_binary(bytes) and byte_size(bytes) <= 4096 do
    {:CertificationRequest, info, {:CertificationRequest_signatureAlgorithm, @ed, :asn1_NOVALUE},
     signature} = request = :public_key.der_decode(:CertificationRequest, bytes)

    if :public_key.der_encode(:CertificationRequest, request) != bytes, do: throw(:invalid_csr)

    {:CertificationRequestInfo, :v1, name,
     {:CertificationRequestInfo_subjectPKInfo,
      {:CertificationRequestInfo_subjectPKInfo_algorithm, @ed, :asn1_NOVALUE}, public}, []} = info

    expected =
      if host,
        do: {:rdnSequence, [[{:AttributeTypeAndValue, {2, 5, 4, 3}, {:utf8String, host}}]]},
        else: {:rdnSequence, []}

    if name != expected or byte_size(public) != 32 or
         not :crypto.verify(
           :eddsa,
           :none,
           :public_key.der_encode(:CertificationRequestInfo, info),
           signature,
           [public, :ed25519]
         ),
       do: throw(:invalid_csr)

    {:ok, public}
  rescue
    _ -> {:error, :invalid_csr}
  catch
    _ -> {:error, :invalid_csr}
  end

  def csr_public(_, _), do: {:error, :invalid_csr}

  def inspect_certificate(der) do
    if not is_binary(der) or byte_size(der) > 4096, do: throw(:invalid_identity)
    decoded = :public_key.pkix_decode_cert(der, :otp)

    if :public_key.pkix_encode(:OTPCertificate, decoded, :otp) != der,
      do: throw(:invalid_identity)

    value = cert(decoded, :tbsCertificate)
    pub = tbs(value, :subjectPublicKeyInfo)
    algorithm = spki(pub, :algorithm)

    if public_algorithm(algorithm, :algorithm) != @ed or
         public_algorithm(algorithm, :parameters) != :asn1_NOVALUE,
       do: throw(:invalid_identity)

    {:ECPoint, public} = spki(pub, :subjectPublicKey)
    valid = tbs(value, :validity)
    extensions = tbs(value, :extensions)
    constraints = find_extension(extensions, {2, 5, 29, 19})
    ids = Enum.map(extensions, &extension(&1, :extnID))
    if length(ids) != length(Enum.uniq(ids)), do: throw(:invalid_identity)

    %{
      basic_present:
        constraints != nil and basic(constraints, :pathLenConstraint) == :asn1_NOVALUE,
      unknown_critical:
        Enum.any?(extensions, fn ext ->
          extension(ext, :critical) == true and
            extension(ext, :extnID) not in [{2, 5, 29, 19}, {2, 5, 29, 15}, {2, 5, 29, 37}]
        end),
      public: public,
      subject: tbs(value, :subject),
      issuer: tbs(value, :issuer),
      not_before: seconds(validity(valid, :notBefore)),
      not_after: seconds(validity(valid, :notAfter)),
      ca: constraints != nil and basic(constraints, :cA) == true,
      usage: find_extension(extensions, {2, 5, 29, 15}) || [],
      eku: find_extension(extensions, {2, 5, 29, 37}) || []
    }
  end

  def valid_leaf?(der, issuer, host, now) do
    valid_leaf_profile?(der, issuer, now) and inspect_certificate(der).subject == subject(host)
  rescue
    _ -> false
  catch
    _ -> false
  end

  # Everything valid_leaf?/4 requires except the host match: usable to decide that a
  # certificate is an ordinary end-entity leaf of this issuer.
  def valid_leaf_profile?(der, issuer, now) do
    info = inspect_certificate(der)

    root = inspect_certificate(issuer.certificate)

    root_profile?(root) and root.public == issuer.public and root.subject == issuer.subject and
      root.not_after == issuer.expires_at and
      :public_key.pkix_verify(issuer.certificate, {{:ECPoint, root.public}, {:namedCurve, @ed}}) and
      info.basic_present and not info.unknown_critical and not info.ca and
      info.usage == [:digitalSignature] and
      info.eku == [{1, 3, 6, 1, 5, 5, 7, 3, 2}] and
      info.not_after - info.not_before == @day and
      info.issuer == issuer.subject and info.not_before <= now and
      now < info.not_after and
      now < issuer.expires_at and info.not_after <= issuer.expires_at and
      :public_key.pkix_verify(der, {{:ECPoint, issuer.public}, {:namedCurve, @ed}})
  rescue
    _ -> false
  catch
    _ -> false
  end

  defp root_profile?(info) do
    info.ca and info.basic_present and not info.unknown_critical and
      info.usage == [:keyCertSign] and info.eku == [] and
      info.subject == subject("WorkHarbor client issuer") and
      info.not_after - info.not_before == 5 * 365 * @day
  end

  def subject(host),
    do: {:rdnSequence, [[{:AttributeTypeAndValue, {2, 5, 4, 3}, {:utf8String, host}}]]}

  defp sign(key, public, issuer, subject, start, stop, ca) do
    extensions = [
      extension(extnID: {2, 5, 29, 19}, critical: true, extnValue: basic(cA: ca)),
      extension(
        extnID: {2, 5, 29, 15},
        critical: true,
        extnValue: if(ca, do: [:keyCertSign], else: [:digitalSignature])
      )
    ]

    extensions =
      if ca,
        do: extensions,
        else:
          extensions ++
            [
              extension(
                extnID: {2, 5, 29, 37},
                critical: false,
                extnValue: [{1, 3, 6, 1, 5, 5, 7, 3, 2}]
              )
            ]

    record =
      tbs(
        version: :v3,
        serialNumber: :binary.decode_unsigned(:crypto.strong_rand_bytes(16)) + 1,
        signature: signature(algorithm: @ed),
        issuer: issuer,
        subject: subject,
        validity: validity(notBefore: time(start), notAfter: time(stop)),
        subjectPublicKeyInfo:
          spki(algorithm: public_algorithm(algorithm: @ed), subjectPublicKey: {:ECPoint, public}),
        extensions: extensions
      )

    :public_key.pkix_sign(record, key)
  end

  defp find_extension(exts, oid) do
    case Enum.find(exts, &(extension(&1, :extnID) == oid)) do
      nil -> nil
      ext -> extension(ext, :extnValue)
    end
  end

  defp time(seconds),
    do:
      {:generalTime,
       DateTime.from_unix!(seconds) |> Calendar.strftime("%Y%m%d%H%M%SZ") |> String.to_charlist()}

  defp seconds({kind, chars}) do
    s = List.to_string(chars)

    full =
      if kind == :utcTime,
        do: if(String.slice(s, 0, 2) >= "50", do: "19", else: "20") <> s,
        else: s

    [y, m, d, h, n, s] =
      for {offset, size} <- [{0, 4}, {4, 2}, {6, 2}, {8, 2}, {10, 2}, {12, 2}],
          do: String.slice(full, offset, size) |> String.to_integer()

    :calendar.datetime_to_gregorian_seconds({{y, m, d}, {h, n, s}}) - 62_167_219_200
  end

  defp ensure_dir(dir) do
    case File.mkdir(dir) do
      :ok -> File.chmod(dir, 0o700)
      {:error, :eexist} -> :ok
      error -> error
    end
  end
end
