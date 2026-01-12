import Config

# In test, we don't want start/2 to invoke main() and halt
config :scl_parser_cli, mode: :test
