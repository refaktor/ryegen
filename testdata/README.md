## Some tests for binding generation

### Input Files
| File                          | Function                                                |
|-------------------------------|---------------------------------------------------------|
| *test-name*.in.go             | Go code containing definitions to generate bindings for |
| *test-name*.rye               | Rye program to run                                      |
| *test-name*.expected_output   | Expected output of Rye program                          |
| *test-name*.expected_errors   | Expected converter errors (optional)                    |
| *test-name*.toml              | Ryegen config (optional)                                |


### Generated Files
| File                          | Function                                                     |
|-------------------------------|--------------------------------------------------------------|
| *test-name*.out_convs.go      | Generated type converters between Rye and Go                 |
| *test-name*.out_builtins.go   | Entry point and generated list of builtins exposed to Rye    |

### Compilation
All .go files mentioned above are compiled together to build a Rye interpreter with bindings.
