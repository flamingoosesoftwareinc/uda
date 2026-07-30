from setuptools import setup, find_packages

setup(
    name="hypothesis-jsonschema",
    packages=find_packages("src"),
    package_dir={"": "src"},
)
