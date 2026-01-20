defmodule SCLParser.MixProject do
  use Mix.Project

  def project do
    [
      app: :scl_parser,
      version: "1.0.1",
      elixir: "~> 1.18",
      elixirc_options: [warnings_as_errors: true],
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      test_coverage: [tool: ExCoveralls],
      preferred_cli_env: [
        coveralls: :test,
        "coveralls.detail": :test,
        "coveralls.post": :test,
        "coveralls.html": :test,
        "coveralls.cobertura": :test
      ],
      description: "A line-aware SCL (Simple Configuration Language) parser.",
      package: package()
    ]
  end

  # Run "mix help compile.app" to learn about applications.
  def application do
    [
      extra_applications: [:logger]
    ]
  end

  defp package do
    [
      name: "scl_parser",
      licenses: ["Apache-2.0"],
      files: ~w(lib .formatter.exs mix.exs README* LICENSE*),
      links: %{"GitHub" => "https://github.com/simple-dev/simple-tools"}
    ]
  end

  # Run "mix help deps" to learn about dependencies.
  defp deps do
    [
      {:credo, "1.7.15", only: [:dev, :test], runtime: false},
      {:excoveralls, "0.18.5", only: :test},
      {:ex_doc, "== 0.40.0", only: :dev, runtime: false}
    ]
  end
end
