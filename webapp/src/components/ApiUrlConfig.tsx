import { useState } from 'react'
import {
  HStack,
  Input,
  Button,
  Text,
  Card,
  CardBody,
  useToast,
} from '@chakra-ui/react'
import { useApi } from '~contexts/ApiContext'
import { useQueryClient } from '@tanstack/react-query'

export const ApiUrlConfig = () => {
  const { apiUrl, setApiUrl } = useApi()
  const [inputValue, setInputValue] = useState(apiUrl)
  const queryClient = useQueryClient()
  const toast = useToast()

  const handleUpdate = () => {
    if (inputValue !== apiUrl) {
      setApiUrl(inputValue)
      // Invalidate all queries to refetch with new URL
      queryClient.invalidateQueries()
      toast({
        title: 'API URL Updated',
        description: `Now using: ${inputValue}`,
        status: 'success',
        duration: 3000,
        isClosable: true,
      })
    }
  }

  const handleKeyPress = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      handleUpdate()
    }
  }

  return (
    <Card size="sm">
      <CardBody>
        <HStack spacing={3}>
          <Text fontSize="sm" fontWeight="medium" color="gray.600" whiteSpace="nowrap">
            Sequencer API:
          </Text>
          <Input
            size="sm"
            value={inputValue}
            onChange={(e) => setInputValue(e.target.value)}
            onKeyPress={handleKeyPress}
            placeholder="http://localhost:9090"
            flex="1"
          />
          <Button
            size="sm"
            colorScheme="purple"
            onClick={handleUpdate}
            isDisabled={inputValue === apiUrl || !inputValue}
          >
            Update
          </Button>
        </HStack>
      </CardBody>
    </Card>
  )
}
