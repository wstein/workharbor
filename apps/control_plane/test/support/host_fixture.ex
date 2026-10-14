# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.HostFixture do
  @moduledoc false
  def csr(host \\ nil) do
    {public, private} = :crypto.generate_key(:eddsa, :ed25519)
    {csr_bytes(public, private, host), public}
  end

  def csr_with_key do
    key = :public_key.generate_key({:namedCurve, {1, 3, 101, 112}})
    {csr_bytes(elem(key, 4), elem(key, 2), nil), key}
  end

  defp csr_bytes(public, private, host) do
    subject =
      if host,
        do: {:rdnSequence, [[{:AttributeTypeAndValue, {2, 5, 4, 3}, {:utf8String, host}}]]},
        else: {:rdnSequence, []}

    info =
      {:CertificationRequestInfo, 0, subject,
       {:CertificationRequestInfo_subjectPKInfo,
        {:CertificationRequestInfo_subjectPKInfo_algorithm, {1, 3, 101, 112}, :asn1_NOVALUE},
        public}, []}

    bytes = :public_key.der_encode(:CertificationRequestInfo, info)
    signature = :crypto.sign(:eddsa, :none, bytes, [private, :ed25519])

    :public_key.der_encode(
      :CertificationRequest,
      {:CertificationRequest, info,
       {:CertificationRequest_signatureAlgorithm, {1, 3, 101, 112}, :asn1_NOVALUE}, signature}
    )
  end

  # Resolve trusted OS temp aliases only in fixtures; production private paths
  # refuse every symlink component and never silently resolve them.
  def physical_temp do
    ["/" | parts] = Path.split(System.tmp_dir!())
    physical_parts("/", parts, 0)
  end

  defp physical_parts(path, [], _), do: path

  defp physical_parts(path, [part | rest], depth) when depth < 40 do
    next = Path.join(path, part)

    case File.read_link(next) do
      {:ok, target} ->
        ["/" | resolved] = Path.split(Path.expand(target, path))
        physical_parts("/", resolved ++ rest, depth + 1)

      {:error, :einval} ->
        physical_parts(next, rest, depth)

      _ ->
        raise("unavailable physical fixture directory")
    end
  end
end
